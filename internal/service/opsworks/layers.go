// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package opsworks

import (
	"context"
	"fmt"
	"log"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/opsworks"
	awstypes "github.com/aws/aws-sdk-go-v2/service/opsworks/types"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/retry"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/structure"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
	"github.com/hashicorp/terraform-provider-aws/internal/conns"
	"github.com/hashicorp/terraform-provider-aws/internal/create"
	"github.com/hashicorp/terraform-provider-aws/internal/enum"
	"github.com/hashicorp/terraform-provider-aws/internal/errs"
	"github.com/hashicorp/terraform-provider-aws/internal/errs/sdkdiag"
	"github.com/hashicorp/terraform-provider-aws/internal/flex"
	tfmaps "github.com/hashicorp/terraform-provider-aws/internal/maps"
	tftags "github.com/hashicorp/terraform-provider-aws/internal/tags"
	"github.com/hashicorp/terraform-provider-aws/internal/tfresource"
	"github.com/hashicorp/terraform-provider-aws/internal/verify"
	"github.com/hashicorp/terraform-provider-aws/names"
)

// OpsWorks has a single concept of "layer" which represents several different
// layer types. The differences between these are in some extra properties that
// get packed into an "Attributes" map, but in the OpsWorks UI these are presented
// as first-class options, and so Terraform prefers to expose them this way and
// hide the implementation detail that they are all packed into a single type
// in the underlying API.
//
// This file contains utilities that are shared between all of the concrete
// layer resource types, which have names matching aws_opsworks_*_layer .

type opsworksLayerTypeAttribute struct {
	AttrName     awstypes.LayerAttributesKeys
	Type         schema.ValueType
	Default      any
	ForceNew     bool
	Required     bool
	ValidateFunc schema.SchemaValidateFunc
	WriteOnly    bool
}

type opsworksLayerTypeAttributeMap map[string]*opsworksLayerTypeAttribute

type opsworksLayerType struct {
	TypeName         awstypes.LayerType
	DefaultLayerName string
	Attributes       opsworksLayerTypeAttributeMap
	CustomShortName  bool
}

func (lt *opsworksLayerType) resourceSchema() *schema.Resource {
	resourceSchema := map[string]*schema.Schema{
		names.AttrARN: {
			Type:     schema.TypeString,
			Computed: true,
		},
		"auto_assign_elastic_ips": {
			Type:     schema.TypeBool,
			Optional: true,
			Default:  false,
		},
		"auto_assign_public_ips": {
			Type:     schema.TypeBool,
			Optional: true,
			Default:  false,
		},
		"auto_healing": {
			Type:     schema.TypeBool,
			Optional: true,
			Default:  true,
		},
		"cloudwatch_configuration": {
			Type:     schema.TypeList,
			Optional: true,
			MaxItems: 1,
			DiffSuppressFunc: func(k, old, new string, d *schema.ResourceData) bool {
				if old == "1" && new == "0" && !d.Get("cloudwatch_configuration.0.enabled").(bool) {
					return true
				}
				return false
			},
			Elem: &schema.Resource{
				Schema: map[string]*schema.Schema{
					names.AttrEnabled: {
						Type:     schema.TypeBool,
						Optional: true,
						DiffSuppressFunc: func(k, old, new string, d *schema.ResourceData) bool {
							if old == "false" && new == "" {
								return true
							}
							return false
						},
					},
					"log_streams": {
						Type:     schema.TypeList,
						Optional: true,
						DiffSuppressFunc: func(k, old, new string, d *schema.ResourceData) bool {
							if old == "1" && new == "0" && !d.Get("cloudwatch_configuration.0.enabled").(bool) {
								return true
							}
							return false
						},
						Elem: &schema.Resource{
							Schema: map[string]*schema.Schema{
								"batch_count": {
									Type:         schema.TypeInt,
									Default:      1000,
									Optional:     true,
									ValidateFunc: validation.IntAtMost(10000),
								},
								"batch_size": {
									Type:         schema.TypeInt,
									Default:      32768,
									Optional:     true,
									ValidateFunc: validation.IntAtMost(1048576),
								},
								"buffer_duration": {
									Type:         schema.TypeInt,
									Default:      5000,
									Optional:     true,
									ValidateFunc: validation.IntAtLeast(5000),
								},
								"datetime_format": {
									Type:     schema.TypeString,
									Optional: true,
								},
								"encoding": {
									Type:             schema.TypeString,
									Optional:         true,
									Default:          awstypes.CloudWatchLogsEncodingUtf8,
									ValidateDiagFunc: enum.Validate[awstypes.CloudWatchLogsEncoding](),
								},
								"file": {
									Type:     schema.TypeString,
									Required: true,
								},
								"file_fingerprint_lines": {
									Type:     schema.TypeString,
									Optional: true,
									Default:  "1",
								},
								"initial_position": {
									Type:             schema.TypeString,
									Optional:         true,
									Default:          awstypes.CloudWatchLogsInitialPositionStartOfFile,
									ValidateDiagFunc: enum.Validate[awstypes.CloudWatchLogsInitialPosition](),
								},
								names.AttrLogGroupName: {
									Type:     schema.TypeString,
									Required: true,
								},
								"multiline_start_pattern": {
									Type:     schema.TypeString,
									Optional: true,
								},
								"time_zone": {
									Type:             schema.TypeString,
									Optional:         true,
									ValidateDiagFunc: enum.Validate[awstypes.CloudWatchLogsTimeZone](),
								},
							},
						},
					},
				},
			},
		},
		"custom_configure_recipes": {
			Type:     schema.TypeList,
			Optional: true,
			Elem:     &schema.Schema{Type: schema.TypeString},
		},
		"custom_deploy_recipes": {
			Type:     schema.TypeList,
			Optional: true,
			Elem:     &schema.Schema{Type: schema.TypeString},
		},
		"custom_instance_profile_arn": {
			Type:         schema.TypeString,
			Optional:     true,
			ValidateFunc: verify.ValidARN,
		},
		"custom_json": {
			Type:         schema.TypeString,
			ValidateFunc: validation.StringIsJSON,
			StateFunc: func(v any) string {
				json, _ := structure.NormalizeJsonString(v)
				return json
			},
			Optional: true,
		},
		"custom_security_group_ids": {
			Type:     schema.TypeSet,
			Optional: true,
			Elem:     &schema.Schema{Type: schema.TypeString},
		},
		"custom_setup_recipes": {
			Type:     schema.TypeList,
			Optional: true,
			Elem:     &schema.Schema{Type: schema.TypeString},
		},
		"custom_shutdown_recipes": {
			Type:     schema.TypeList,
			Optional: true,
			Elem:     &schema.Schema{Type: schema.TypeString},
		},
		"custom_undeploy_recipes": {
			Type:     schema.TypeList,
			Optional: true,
			Elem:     &schema.Schema{Type: schema.TypeString},
		},
		"drain_elb_on_shutdown": {
			Type:     schema.TypeBool,
			Optional: true,
			Default:  true,
		},
		"ebs_volume": {
			Type:     schema.TypeSet,
			Optional: true,
			Computed: true,
			Elem: &schema.Resource{
				Schema: map[string]*schema.Schema{
					names.AttrEncrypted: {
						Type:     schema.TypeBool,
						Optional: true,
						Default:  false,
					},
					names.AttrIOPS: {
						Type:     schema.TypeInt,
						Optional: true,
						Default:  0,
					},
					"mount_point": {
						Type:     schema.TypeString,
						Required: true,
					},
					"number_of_disks": {
						Type:     schema.TypeInt,
						Required: true,
					},
					"raid_level": {
						Type:     schema.TypeString,
						Optional: true,
						Default:  "",
					},
					names.AttrSize: {
						Type:     schema.TypeInt,
						Required: true,
					},
					names.AttrType: {
						Type:     schema.TypeString,
						Optional: true,
						Default:  "standard",
						ValidateFunc: validation.StringInSlice([]string{
							"standard",
							"io1",
							"gp2",
							"st1",
							"sc1",
						}, false),
					},
				},
			},
			Set: func(v any) int {
				m := v.(map[string]any)
				return create.StringHashcode(m["mount_point"].(string))
			},
		},
		"elastic_load_balancer": {
			Type:     schema.TypeString,
			Optional: true,
		},
		"instance_shutdown_timeout": {
			Type:     schema.TypeInt,
			Optional: true,
			Default:  120,
		},
		"install_updates_on_boot": {
			Type:     schema.TypeBool,
			Optional: true,
			Default:  true,
		},
		"load_based_auto_scaling": {
			Type:     schema.TypeList,
			Optional: true,
			Computed: true,
			MaxItems: 1,
			Elem: &schema.Resource{
				Schema: map[string]*schema.Schema{
					"downscaling": {
						Type:     schema.TypeList,
						Optional: true,
						Computed: true,
						MaxItems: 1,
						Elem: &schema.Resource{
							Schema: map[string]*schema.Schema{
								"alarms": {
									Type:     schema.TypeList,
									Optional: true,
									Elem:     &schema.Schema{Type: schema.TypeString},
									MaxItems: 5,
								},
								"cpu_threshold": {
									Type:     schema.TypeFloat,
									Optional: true,
									Default:  30.0,
								},
								"ignore_metrics_time": {
									Type:         schema.TypeInt,
									Optional:     true,
									Default:      10,
									ValidateFunc: validation.IntBetween(1, 100),
								},
								names.AttrInstanceCount: {
									Type:     schema.TypeInt,
									Optional: true,
									Default:  1,
								},
								"load_threshold": {
									Type:     schema.TypeFloat,
									Optional: true,
								},
								"memory_threshold": {
									Type:     schema.TypeFloat,
									Optional: true,
								},
								"thresholds_wait_time": {
									Type:         schema.TypeInt,
									Optional:     true,
									Default:      10,
									ValidateFunc: validation.IntBetween(1, 100),
								},
							},
						},
					},
					"enable": {
						Type:     schema.TypeBool,
						Optional: true,
					},
					"upscaling": {
						Type:     schema.TypeList,
						Optional: true,
						Computed: true,
						MaxItems: 1,
						Elem: &schema.Resource{
							Schema: map[string]*schema.Schema{
								"alarms": {
									Type:     schema.TypeList,
									Optional: true,
									Elem:     &schema.Schema{Type: schema.TypeString},
									MaxItems: 5,
								},
								"cpu_threshold": {
									Type:     schema.TypeFloat,
									Optional: true,
									Default:  80.0,
								},
								"ignore_metrics_time": {
									Type:         schema.TypeInt,
									Optional:     true,
									Default:      5,
									ValidateFunc: validation.IntBetween(1, 100),
								},
								names.AttrInstanceCount: {
									Type:     schema.TypeInt,
									Optional: true,
									Default:  1,
								},
								"load_threshold": {
									Type:     schema.TypeFloat,
									Optional: true,
								},
								"memory_threshold": {
									Type:     schema.TypeFloat,
									Optional: true,
								},
								"thresholds_wait_time": {
									Type:         schema.TypeInt,
									Optional:     true,
									Default:      5,
									ValidateFunc: validation.IntBetween(1, 100),
								},
							},
						},
					},
				},
			},
		},
		"system_packages": {
			Type:     schema.TypeSet,
			Optional: true,
			Elem:     &schema.Schema{Type: schema.TypeString},
		},
		"stack_id": {
			Type:     schema.TypeString,
			ForceNew: true,
			Required: true,
		},
		names.AttrTags:    tftags.TagsSchema(),
		names.AttrTagsAll: tftags.TagsSchemaComputed(),
		"use_ebs_optimized_instances": {
			Type:     schema.TypeBool,
			Optional: true,
			Default:  false,
		},
	}

	if lt.CustomShortName {
		resourceSchema["short_name"] = &schema.Schema{
			Type:     schema.TypeString,
			Required: true,
		}
	}

	if lt.DefaultLayerName != "" {
		resourceSchema[names.AttrName] = &schema.Schema{
			Type:     schema.TypeString,
			Optional: true,
			Default:  lt.DefaultLayerName,
		}
	} else {
		resourceSchema[names.AttrName] = &schema.Schema{
			Type:     schema.TypeString,
			Required: true,
		}
	}

	for key, def := range lt.Attributes {
		resourceSchema[key] = &schema.Schema{
			Type:         def.Type,
			Default:      def.Default,
			ForceNew:     def.ForceNew,
			Required:     def.Required,
			Optional:     !def.Required,
			ValidateFunc: def.ValidateFunc,
		}
	}

	return &schema.Resource{
		DeprecationMessage: "This resource is deprecated and will be removed in the next major version of the AWS Provider. Consider the AWS Systems Manager service instead.",
		CreateWithoutTimeout: func(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
			return lt.Create(ctx, d, meta)
		},
		ReadWithoutTimeout: func(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
			return lt.Read(ctx, d, meta)
		},
		UpdateWithoutTimeout: func(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
			return lt.Update(ctx, d, meta)
		},
		DeleteWithoutTimeout: func(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
			return lt.Delete(ctx, d, meta)
		},

		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		SchemaFunc: func() map[string]*schema.Schema {
			return resourceSchema
		},
	}
}

func (lt *opsworksLayerType) Create(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
	var diags diag.Diagnostics
	conn := meta.(*conns.AWSClient).OpsWorksClient(ctx)

	attributes, err := lt.Attributes.resourceDataToAPIAttributes(d)

	if err != nil {
		return sdkdiag.AppendFromErr(diags, err)
	}

	name := d.Get(names.AttrName).(string)
	input := &opsworks.CreateLayerInput{
		Attributes:           attributes,
		AutoAssignElasticIps: aws.Bool(d.Get("auto_assign_elastic_ips").(bool)),
		AutoAssignPublicIps:  aws.Bool(d.Get("auto_assign_public_ips").(bool)),
		CustomRecipes:        &awstypes.Recipes{},
		EnableAutoHealing:    aws.Bool(d.Get("auto_healing").(bool)),
		InstallUpdatesOnBoot: aws.Bool(d.Get("install_updates_on_boot").(bool)),
		LifecycleEventConfiguration: &awstypes.LifecycleEventConfiguration{
			Shutdown: &awstypes.ShutdownEventConfiguration{
				DelayUntilElbConnectionsDrained: aws.Bool(d.Get("drain_elb_on_shutdown").(bool)),
			},
		},
		Name:                     aws.String(name),
		Type:                     lt.TypeName,
		StackId:                  aws.String(d.Get("stack_id").(string)),
		UseEbsOptimizedInstances: aws.Bool(d.Get("use_ebs_optimized_instances").(bool)),
	}

	if v, ok := d.GetOk("cloudwatch_configuration"); ok && len(v.([]any)) > 0 && v.([]any)[0] != nil {
		input.CloudWatchLogsConfiguration = expandCloudWatchLogsConfiguration(v.([]any)[0].(map[string]any))
	}

	if v, ok := d.GetOk("custom_configure_recipes"); ok && len(v.([]any)) > 0 {
		input.CustomRecipes.Configure = flex.ExpandStringValueList(v.([]any))
	}

	if v, ok := d.GetOk("custom_deploy_recipes"); ok && len(v.([]any)) > 0 {
		input.CustomRecipes.Deploy = flex.ExpandStringValueList(v.([]any))
	}

	if v, ok := d.GetOk("custom_instance_profile_arn"); ok {
		input.CustomInstanceProfileArn = aws.String(v.(string))
	}

	if v, ok := d.GetOk("custom_json"); ok {
		input.CustomJson = aws.String(v.(string))
	}

	if v, ok := d.GetOk("custom_security_group_ids"); ok && v.(*schema.Set).Len() > 0 {
		input.CustomSecurityGroupIds = flex.ExpandStringValueSet(v.(*schema.Set))
	}

	if v, ok := d.GetOk("custom_setup_recipes"); ok && len(v.([]any)) > 0 {
		input.CustomRecipes.Setup = flex.ExpandStringValueList(v.([]any))
	}

	if v, ok := d.GetOk("custom_shutdown_recipes"); ok && len(v.([]any)) > 0 {
		input.CustomRecipes.Shutdown = flex.ExpandStringValueList(v.([]any))
	}

	if v, ok := d.GetOk("custom_undeploy_recipes"); ok && len(v.([]any)) > 0 {
		input.CustomRecipes.Undeploy = flex.ExpandStringValueList(v.([]any))
	}

	if v, ok := d.GetOk("ebs_volume"); ok && v.(*schema.Set).Len() > 0 {
		input.VolumeConfigurations = expandVolumeConfigurations(v.(*schema.Set).List())
	}

	if v, ok := d.GetOk("instance_shutdown_timeout"); ok {
		input.LifecycleEventConfiguration.Shutdown.ExecutionTimeout = aws.Int32(int32(v.(int)))
	}

	if lt.CustomShortName {
		input.Shortname = aws.String(d.Get("short_name").(string))
	} else {
		input.Shortname = aws.String(string(lt.TypeName))
	}

	if v, ok := d.GetOk("system_packages"); ok && v.(*schema.Set).Len() > 0 {
		input.Packages = flex.ExpandStringValueSet(v.(*schema.Set))
	}

	if v, ok := d.GetOk("ecs_cluster_arn"); ok {
		arn := v.(string)
		_, err := conn.RegisterEcsCluster(ctx, &opsworks.RegisterEcsClusterInput{
			EcsClusterArn: aws.String(arn),
			StackId:       input.StackId,
		})

		if err != nil {
			return sdkdiag.AppendErrorf(diags, "registering OpsWorks Layer (%s) ECS Cluster (%s): %s", name, arn, err)
		}
	}

	log.Printf("[DEBUG] Creating OpsWorks Layer: %#v", input)
	output, err := conn.CreateLayer(ctx, input)

	if err != nil {
		return sdkdiag.AppendErrorf(diags, "creating OpsWorks Layer (%s): %s", name, err)
	}

	d.SetId(aws.ToString(output.LayerId))

	if v, ok := d.GetOk("elastic_load_balancer"); ok {
		v := v.(string)
		_, err := conn.AttachElasticLoadBalancer(ctx, &opsworks.AttachElasticLoadBalancerInput{
			ElasticLoadBalancerName: aws.String(v),
			LayerId:                 aws.String(d.Id()),
		})

		if err != nil {
			return sdkdiag.AppendErrorf(diags, "attaching OpsWorks Layer (%s) load balancer (%s): %s", d.Id(), v, err)
		}
	}

	if v, ok := d.GetOk("load_based_auto_scaling"); ok && len(v.([]any)) > 0 && v.([]any)[0] != nil {
		input := expandSetLoadBasedAutoScalingInput(v.([]any)[0].(map[string]any))
		input.LayerId = aws.String(d.Id())

		_, err := conn.SetLoadBasedAutoScaling(ctx, input)

		if err != nil {
			return sdkdiag.AppendErrorf(diags, "setting OpsWorks Layer (%s) load-based auto scaling configuration: %s", d.Id(), err)
		}
	}

	if tags := keyValueTags(ctx, getTagsIn(ctx)); len(tags) > 0 {
		layer, err := findLayerByID(ctx, conn, d.Id())

		if err != nil {
			return sdkdiag.AppendErrorf(diags, "reading OpsWorks Layer (%s): %s", d.Id(), err)
		}

		arn := aws.ToString(layer.Arn)
		if err := updateTags(ctx, conn, arn, nil, tags); err != nil {
			return sdkdiag.AppendErrorf(diags, "adding OpsWorks Layer (%s) tags: %s", arn, err)
		}
	}

	return append(diags, lt.Read(ctx, d, meta)...)
}

func (lt *opsworksLayerType) Read(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
	var diags diag.Diagnostics
	conn := meta.(*conns.AWSClient).OpsWorksClient(ctx)

	layer, err := findLayerByID(ctx, conn, d.Id())

	if !d.IsNewResource() && tfresource.NotFound(err) {
		log.Printf("[WARN] OpsWorks Layer %s not found, removing from state", d.Id())
		d.SetId("")
		return nil
	}

	if err != nil {
		return sdkdiag.AppendErrorf(diags, "reading OpsWorks Layer (%s): %s", d.Id(), err)
	}

	arn := aws.ToString(layer.Arn)
	d.Set(names.AttrARN, arn)
	d.Set("auto_assign_elastic_ips", layer.AutoAssignElasticIps)
	d.Set("auto_assign_public_ips", layer.AutoAssignPublicIps)
	d.Set("auto_healing", layer.EnableAutoHealing)
	if layer.CloudWatchLogsConfiguration != nil {
		if err := d.Set("cloudwatch_configuration", []any{flattenCloudWatchLogsConfiguration(layer.CloudWatchLogsConfiguration)}); err != nil {
			return sdkdiag.AppendErrorf(diags, "setting cloudwatch_configuration: %s", err)
		}
	} else {
		d.Set("cloudwatch_configuration", nil)
	}
	if layer.CustomRecipes == nil {
		d.Set("custom_configure_recipes", nil)
		d.Set("custom_deploy_recipes", nil)
		d.Set("custom_setup_recipes", nil)
		d.Set("custom_shutdown_recipes", nil)
		d.Set("custom_undeploy_recipes", nil)
	} else {
		d.Set("custom_configure_recipes", layer.CustomRecipes.Configure)
		d.Set("custom_deploy_recipes", layer.CustomRecipes.Deploy)
		d.Set("custom_setup_recipes", layer.CustomRecipes.Setup)
		d.Set("custom_shutdown_recipes", layer.CustomRecipes.Shutdown)
		d.Set("custom_undeploy_recipes", layer.CustomRecipes.Undeploy)
	}
	d.Set("custom_instance_profile_arn", layer.CustomInstanceProfileArn)
	if layer.CustomJson == nil {
		d.Set("custom_json", "")
	} else {
		policy, err := structure.NormalizeJsonString(aws.ToString(layer.CustomJson))
		if err != nil {
			return sdkdiag.AppendErrorf(diags, "policy contains an invalid JSON: %s", err)
		}
		d.Set("custom_json", policy)
	}
	d.Set("custom_security_group_ids", layer.CustomSecurityGroupIds)
	if layer.LifecycleEventConfiguration == nil || layer.LifecycleEventConfiguration.Shutdown == nil {
		d.Set("drain_elb_on_shutdown", nil)
		d.Set("instance_shutdown_timeout", nil)
	} else {
		d.Set("drain_elb_on_shutdown", layer.LifecycleEventConfiguration.Shutdown.DelayUntilElbConnectionsDrained)
		d.Set("instance_shutdown_timeout", layer.LifecycleEventConfiguration.Shutdown.ExecutionTimeout)
	}
	if err := d.Set("ebs_volume", flattenVolumeConfigurations(layer.VolumeConfigurations)); err != nil {
		return sdkdiag.AppendErrorf(diags, "setting ebs_volume: %s", err)
	}
	d.Set("install_updates_on_boot", layer.InstallUpdatesOnBoot)
	d.Set(names.AttrName, layer.Name)
	if lt.CustomShortName {
		d.Set("short_name", layer.Shortname)
	}
	d.Set("system_packages", layer.Packages)
	d.Set("stack_id", layer.StackId)
	d.Set("use_ebs_optimized_instances", layer.UseEbsOptimizedInstances)

	if err := lt.Attributes.apiAttributesToResourceData(layer.Attributes, d); err != nil {
		return sdkdiag.AppendFromErr(diags, err)
	}

	loadBalancer, err := findElasticLoadBalancerByLayerID(ctx, conn, d.Id())

	if err == nil {
		d.Set("elastic_load_balancer", loadBalancer.ElasticLoadBalancerName)
	} else if tfresource.NotFound(err) {
		d.Set("elastic_load_balancer", nil)
	} else {
		return sdkdiag.AppendErrorf(diags, "reading OpsWorks Layer (%s) load balancers: %s", d.Id(), err)
	}

	loadBasedAutoScalingConfiguration, err := findLoadBasedAutoScalingConfigurationByLayerID(ctx, conn, d.Id())

	if err == nil {
		if err := d.Set("load_based_auto_scaling", []any{flattenLoadBasedAutoScalingConfiguration(loadBasedAutoScalingConfiguration)}); err != nil {
			return sdkdiag.AppendErrorf(diags, "setting load_based_auto_scaling: %s", err)
		}
	} else if tfresource.NotFound(err) {
		d.Set("load_based_auto_scaling", nil)
	} else {
		return sdkdiag.AppendErrorf(diags, "reading OpsWorks Layer (%s) load-based auto scaling configurations: %s", d.Id(), err)
	}

	return nil
}

func (lt *opsworksLayerType) Update(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
	var diags diag.Diagnostics
	conn := meta.(*conns.AWSClient).OpsWorksClient(ctx)

	if d.HasChangesExcept("elastic_load_balancer", "load_based_auto_scaling", names.AttrTags, names.AttrTagsAll) {
		input := &opsworks.UpdateLayerInput{
			LayerId: aws.String(d.Id()),
		}

		if d.HasChanges(tfmaps.Keys(lt.Attributes)...) {
			attributes, err := lt.Attributes.resourceDataToAPIAttributes(d)

			if err != nil {
				return sdkdiag.AppendFromErr(diags, err)
			}

			input.Attributes = attributes
		}

		if d.HasChanges("auto_assign_elastic_ips") {
			input.AutoAssignElasticIps = aws.Bool(d.Get("auto_assign_elastic_ips").(bool))
		}

		if d.HasChanges("auto_assign_public_ips") {
			input.AutoAssignPublicIps = aws.Bool(d.Get("auto_assign_public_ips").(bool))
		}

		if d.HasChanges("auto_healing") {
			input.EnableAutoHealing = aws.Bool(d.Get("auto_assign_public_ips").(bool))
		}

		if d.HasChanges("cloudwatch_configuration") {
			if v, ok := d.GetOk("cloudwatch_configuration"); ok && len(v.([]any)) > 0 && v.([]any)[0] != nil {
				input.CloudWatchLogsConfiguration = expandCloudWatchLogsConfiguration(v.([]any)[0].(map[string]any))
			}
		}

		if d.HasChanges("custom_configure_recipes", "custom_deploy_recipes", "custom_setup_recipes", "custom_shutdown_recipes", "custom_undeploy_recipes") {
			apiObject := &awstypes.Recipes{}

			if d.HasChanges("custom_configure_recipes") {
				apiObject.Configure = flex.ExpandStringValueList(d.Get("custom_configure_recipes").([]any))
			}

			if d.HasChanges("custom_deploy_recipes") {
				apiObject.Deploy = flex.ExpandStringValueList(d.Get("custom_deploy_recipes").([]any))
			}

			if d.HasChanges("custom_setup_recipes") {
				apiObject.Setup = flex.ExpandStringValueList(d.Get("custom_setup_recipes").([]any))
			}

			if d.HasChanges("custom_shutdown_recipes") {
				apiObject.Shutdown = flex.ExpandStringValueList(d.Get("custom_shutdown_recipes").([]any))
			}

			if d.HasChanges("custom_undeploy_recipes") {
				apiObject.Undeploy = flex.ExpandStringValueList(d.Get("custom_undeploy_recipes").([]any))
			}

			input.CustomRecipes = apiObject
		}

		if d.HasChanges("custom_instance_profile_arn") {
			input.CustomInstanceProfileArn = aws.String(d.Get("custom_instance_profile_arn").(string))
		}

		if d.HasChange("custom_json") {
			input.CustomJson = aws.String(d.Get("custom_json").(string))
		}

		if d.HasChanges("custom_security_group_ids") {
			input.CustomSecurityGroupIds = flex.ExpandStringValueSet(d.Get("custom_security_group_ids").(*schema.Set))
		}

		if d.HasChanges("drain_elb_on_shutdown", "instance_shutdown_timeout") {
			input.LifecycleEventConfiguration = &awstypes.LifecycleEventConfiguration{
				Shutdown: &awstypes.ShutdownEventConfiguration{
					DelayUntilElbConnectionsDrained: aws.Bool(d.Get("drain_elb_on_shutdown").(bool)),
					ExecutionTimeout:                aws.Int32(int32(d.Get("instance_shutdown_timeout").(int))),
				},
			}
		}

		if d.HasChanges("ebs_volume") {
			if v, ok := d.GetOk("ebs_volume"); ok && v.(*schema.Set).Len() > 0 {
				input.VolumeConfigurations = expandVolumeConfigurations(v.(*schema.Set).List())
			}
		}

		if d.HasChanges("install_updates_on_boot") {
			input.InstallUpdatesOnBoot = aws.Bool(d.Get("install_updates_on_boot").(bool))
		}

		if d.HasChange(names.AttrName) {
			input.Name = aws.String(d.Get(names.AttrName).(string))
		}

		if d.HasChange("short_name") {
			input.Shortname = aws.String(d.Get("short_name").(string))
		}

		if d.HasChanges("system_packages") {
			input.Packages = flex.ExpandStringValueSet(d.Get("system_packages").(*schema.Set))
		}

		if d.HasChanges("use_ebs_optimized_instances") {
			input.UseEbsOptimizedInstances = aws.Bool(d.Get("install_updates_on_boot").(bool))
		}

		log.Printf("[DEBUG] Updating OpsWorks Layer: %#v", input)
		_, err := conn.UpdateLayer(ctx, input)

		if err != nil {
			return sdkdiag.AppendErrorf(diags, "updating OpsWorks Layer (%s): %s", d.Id(), err)
		}
	}

	if d.HasChange("elastic_load_balancer") {
		o, n := d.GetChange("elastic_load_balancer")

		if v := o.(string); v != "" {
			_, err := conn.DetachElasticLoadBalancer(ctx, &opsworks.DetachElasticLoadBalancerInput{
				ElasticLoadBalancerName: aws.String(v),
				LayerId:                 aws.String(d.Id()),
			})

			if err != nil {
				return sdkdiag.AppendErrorf(diags, "detaching OpsWorks Layer (%s) load balancer (%s): %s", d.Id(), v, err)
			}
		}

		if v := n.(string); v != "" {
			_, err := conn.AttachElasticLoadBalancer(ctx, &opsworks.AttachElasticLoadBalancerInput{
				ElasticLoadBalancerName: aws.String(v),
				LayerId:                 aws.String(d.Id()),
			})

			if err != nil {
				return sdkdiag.AppendErrorf(diags, "attaching OpsWorks Layer (%s) load balancer (%s): %s", d.Id(), v, err)
			}
		}
	}

	if d.HasChange("load_based_auto_scaling") {
		input := expandSetLoadBasedAutoScalingInput(d.Get("load_based_auto_scaling").([]any)[0].(map[string]any))
		input.LayerId = aws.String(d.Id())

		_, err := conn.SetLoadBasedAutoScaling(ctx, input)

		if err != nil {
			return sdkdiag.AppendErrorf(diags, "setting OpsWorks Layer (%s) load-based auto scaling configuration: %s", d.Id(), err)
		}
	}

	return append(diags, lt.Read(ctx, d, meta)...)
}

func (lt *opsworksLayerType) Delete(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
	var diags diag.Diagnostics
	conn := meta.(*conns.AWSClient).OpsWorksClient(ctx)

	log.Printf("[DEBUG] Deleting OpsWorks Layer: %s", d.Id())
	_, err := conn.DeleteLayer(ctx, &opsworks.DeleteLayerInput{
		LayerId: aws.String(d.Id()),
	})

	if errs.IsA[*awstypes.ResourceNotFoundException](err) {
		return nil
	}

	if err != nil {
		return sdkdiag.AppendErrorf(diags, "deleting OpsWorks Layer (%s): %s", d.Id(), err)
	}

	if v, ok := d.GetOk("ecs_cluster_arn"); ok {
		arn := v.(string)
		_, err := conn.DeregisterEcsCluster(ctx, &opsworks.DeregisterEcsClusterInput{
			EcsClusterArn: aws.String(arn),
		})

		if err != nil {
			return sdkdiag.AppendErrorf(diags, "deregistering OpsWorks Layer (%s) ECS Cluster (%s): %s", d.Id(), arn, err)
		}
	}

	return nil
}

func findLayerByID(ctx context.Context, conn *opsworks.Client, id string) (*awstypes.Layer, error) {
	input := &opsworks.DescribeLayersInput{
		LayerIds: []string{id},
	}

	output, err := conn.DescribeLayers(ctx, input)

	if errs.IsA[*awstypes.ResourceNotFoundException](err) {
		return nil, &retry.NotFoundError{
			LastError:   err,
			LastRequest: input,
		}
	}

	if err != nil {
		return nil, err
	}

	if output == nil || len(output.Layers) == 0 {
		return nil, tfresource.NewEmptyResultError(input)
	}

	if count := len(output.Layers); count > 1 {
		return nil, tfresource.NewTooManyResultsError(count, input)
	}

	return tfresource.AssertSingleValueResult(output.Layers)
}

func findElasticLoadBalancerByLayerID(ctx context.Context, conn *opsworks.Client, id string) (*awstypes.ElasticLoadBalancer, error) {
	input := &opsworks.DescribeElasticLoadBalancersInput{
		LayerIds: []string{id},
	}

	output, err := conn.DescribeElasticLoadBalancers(ctx, input)

	if err != nil {
		return nil, err
	}

	if output == nil || len(output.ElasticLoadBalancers) == 0 {
		return nil, tfresource.NewEmptyResultError(input)
	}

	if count := len(output.ElasticLoadBalancers); count > 1 {
		return nil, tfresource.NewTooManyResultsError(count, input)
	}

	return tfresource.AssertSingleValueResult(output.ElasticLoadBalancers)
}

func findLoadBasedAutoScalingConfigurationByLayerID(ctx context.Context, conn *opsworks.Client, id string) (*awstypes.LoadBasedAutoScalingConfiguration, error) {
	input := &opsworks.DescribeLoadBasedAutoScalingInput{
		LayerIds: []string{id},
	}

	output, err := conn.DescribeLoadBasedAutoScaling(ctx, input)

	if err != nil {
		return nil, err
	}

	if output == nil || len(output.LoadBasedAutoScalingConfigurations) == 0 {
		return nil, tfresource.NewEmptyResultError(input)
	}

	if count := len(output.LoadBasedAutoScalingConfigurations); count > 1 {
		return nil, tfresource.NewTooManyResultsError(count, input)
	}

	return tfresource.AssertSingleValueResult(output.LoadBasedAutoScalingConfigurations)
}

func (m opsworksLayerTypeAttributeMap) apiAttributesToResourceData(apiAttributes map[string]string, d *schema.ResourceData) error {
	for k, attr := range m {
		// Ignore write-only attributes; we'll just keep what we already have stored.
		// (The AWS API returns garbage placeholder values for these.)
		if attr.WriteOnly {
			continue
		}

		if v, ok := apiAttributes[string(attr.AttrName)]; ok {
			switch typ := attr.Type; typ {
			case schema.TypeString:
				d.Set(k, v)
			case schema.TypeInt:
				if v, err := strconv.Atoi(v); err == nil {
					d.Set(k, v)
				} else {
					d.Set(k, nil)
				}
			case schema.TypeBool:
				d.Set(k, v != "false")
			default:
				return fmt.Errorf("unsupported OpsWorks Layer (%s) attribute (%s) type: %s", d.Id(), k, typ)
			}
		} else {
			d.Set(k, nil)
		}
	}

	return nil
}

func (m opsworksLayerTypeAttributeMap) resourceDataToAPIAttributes(d *schema.ResourceData) (map[string]string, error) {
	apiAttributes := map[string]string{}

	for k, attr := range m {
		v := d.Get(k)

		switch typ := attr.Type; typ {
		case schema.TypeString:
			apiAttributes[string(attr.AttrName)] = v.(string)
		case schema.TypeInt:
			apiAttributes[string(attr.AttrName)] = strconv.Itoa(v.(int))
		case schema.TypeBool:
			apiAttributes[string(attr.AttrName)] = strconv.FormatBool(v.(bool))
		default:
			return nil, fmt.Errorf("unsupported OpsWorks Layer (%s) attribute (%s) type: %s", d.Id(), k, typ)
		}
	}

	return apiAttributes, nil
}

func expandCloudWatchLogsConfiguration(tfMap map[string]any) *awstypes.CloudWatchLogsConfiguration {
	if tfMap == nil {
		return nil
	}

	apiObject := &awstypes.CloudWatchLogsConfiguration{}

	if v, ok := tfMap[names.AttrEnabled].(bool); ok {
		apiObject.Enabled = aws.Bool(v)
	}

	if v, ok := tfMap["log_streams"].([]any); ok && len(v) > 0 {
		apiObject.LogStreams = expandCloudWatchLogsLogStreams(v)
	}

	return apiObject
}

func expandCloudWatchLogsLogStream(tfMap map[string]any) awstypes.CloudWatchLogsLogStream {
	apiObject := awstypes.CloudWatchLogsLogStream{}

	if v, ok := tfMap["batch_count"].(int); ok && v != 0 {
		apiObject.BatchCount = aws.Int32(int32(v))
	}

	if v, ok := tfMap["batch_size"].(int); ok && v != 0 {
		apiObject.BatchSize = aws.Int32(int32(v))
	}

	if v, ok := tfMap["buffer_duration"].(int); ok && v != 0 {
		apiObject.BufferDuration = aws.Int32(int32(v))
	}

	if v, ok := tfMap["datetime_format"].(string); ok && v != "" {
		apiObject.DatetimeFormat = aws.String(v)
	}

	if v, ok := tfMap["encoding"].(string); ok && v != "" {
		apiObject.Encoding = awstypes.CloudWatchLogsEncoding(v)
	}

	if v, ok := tfMap["file"].(string); ok && v != "" {
		apiObject.File = aws.String(v)
	}

	if v, ok := tfMap["file_fingerprint_lines"].(string); ok && v != "" {
		apiObject.FileFingerprintLines = aws.String(v)
	}

	if v, ok := tfMap["initial_position"].(string); ok && v != "" {
		apiObject.InitialPosition = awstypes.CloudWatchLogsInitialPosition(v)
	}

	if v, ok := tfMap[names.AttrLogGroupName].(string); ok && v != "" {
		apiObject.LogGroupName = aws.String(v)
	}

	if v, ok := tfMap["multiline_start_pattern"].(string); ok && v != "" {
		apiObject.MultiLineStartPattern = aws.String(v)
	}

	if v, ok := tfMap["time_zone"].(string); ok && v != "" {
		apiObject.TimeZone = awstypes.CloudWatchLogsTimeZone(v)
	}

	return apiObject
}

func expandCloudWatchLogsLogStreams(tfList []any) []awstypes.CloudWatchLogsLogStream {
	if len(tfList) == 0 {
		return nil
	}

	var apiObjects []awstypes.CloudWatchLogsLogStream

	for _, tfMapRaw := range tfList {
		tfMap, ok := tfMapRaw.(map[string]any)

		if !ok {
			continue
		}

		apiObjects = append(apiObjects, expandCloudWatchLogsLogStream(tfMap))
	}

	return apiObjects
}

func flattenCloudWatchLogsConfiguration(apiObject *awstypes.CloudWatchLogsConfiguration) map[string]any {
	if apiObject == nil {
		return nil
	}

	tfMap := map[string]any{}

	if v := apiObject.Enabled; v != nil {
		tfMap[names.AttrEnabled] = aws.ToBool(v)
	}

	if v := apiObject.LogStreams; v != nil {
		tfMap["log_streams"] = flattenCloudWatchLogsLogStreams(v)
	}

	return tfMap
}

func flattenCloudWatchLogsLogStream(apiObject awstypes.CloudWatchLogsLogStream) map[string]any {
	tfMap := map[string]any{}

	if v := apiObject.BatchCount; v != nil {
		tfMap["batch_count"] = aws.ToInt32(v)
	}

	if v := apiObject.BatchSize; v != nil {
		tfMap["batch_size"] = aws.ToInt32(v)
	}

	if v := apiObject.BufferDuration; v != nil {
		tfMap["buffer_duration"] = aws.ToInt32(v)
	}

	if v := apiObject.DatetimeFormat; v != nil {
		tfMap["datetime_format"] = aws.ToString(v)
	}

	tfMap["encoding"] = apiObject.Encoding

	if v := apiObject.File; v != nil {
		tfMap["file"] = aws.ToString(v)
	}

	if v := apiObject.FileFingerprintLines; v != nil {
		tfMap["file_fingerprint_lines"] = aws.ToString(v)
	}

	tfMap["initial_position"] = apiObject.InitialPosition

	if v := apiObject.LogGroupName; v != nil {
		tfMap[names.AttrLogGroupName] = aws.ToString(v)
	}

	if v := apiObject.MultiLineStartPattern; v != nil {
		tfMap["multiline_start_pattern"] = aws.ToString(v)
	}

	tfMap["time_zone"] = apiObject.TimeZone

	return tfMap
}

func flattenCloudWatchLogsLogStreams(apiObjects []awstypes.CloudWatchLogsLogStream) []any {
	if len(apiObjects) == 0 {
		return nil
	}

	var tfList []any

	for _, apiObject := range apiObjects {
		tfList = append(tfList, flattenCloudWatchLogsLogStream(apiObject))
	}

	return tfList
}

func expandVolumeConfiguration(tfMap map[string]any) awstypes.VolumeConfiguration {
	apiObject := awstypes.VolumeConfiguration{}

	if v, ok := tfMap[names.AttrEncrypted].(bool); ok {
		apiObject.Encrypted = aws.Bool(v)
	}

	if v, ok := tfMap[names.AttrIOPS].(int); ok && v != 0 {
		apiObject.Iops = aws.Int32(int32(v))
	}

	if v, ok := tfMap["mount_point"].(string); ok && v != "" {
		apiObject.MountPoint = aws.String(v)
	}

	if v, ok := tfMap["number_of_disks"].(int); ok && v != 0 {
		apiObject.NumberOfDisks = aws.Int32(int32(v))
	}

	if v, ok := tfMap["raid_level"].(string); ok && v != "" {
		if v, err := strconv.ParseInt(v, 10, 32); err == nil {
			apiObject.RaidLevel = aws.Int32(int32(v))
		}
	}

	if v, ok := tfMap[names.AttrSize].(int); ok && v != 0 {
		apiObject.Size = aws.Int32(int32(v))
	}

	if v, ok := tfMap[names.AttrType].(string); ok && v != "" {
		apiObject.VolumeType = aws.String(v)
	}

	return apiObject
}

func expandVolumeConfigurations(tfList []any) []awstypes.VolumeConfiguration {
	if len(tfList) == 0 {
		return nil
	}

	var apiObjects []awstypes.VolumeConfiguration

	for _, tfMapRaw := range tfList {
		tfMap, ok := tfMapRaw.(map[string]any)

		if !ok {
			continue
		}

		apiObjects = append(apiObjects, expandVolumeConfiguration(tfMap))
	}

	return apiObjects
}

func flattenVolumeConfiguration(apiObject awstypes.VolumeConfiguration) map[string]any {
	tfMap := map[string]any{}

	if v := apiObject.Encrypted; v != nil {
		tfMap[names.AttrEncrypted] = aws.ToBool(v)
	}

	if v := apiObject.Iops; v != nil {
		tfMap[names.AttrIOPS] = aws.ToInt32(v)
	}

	if v := apiObject.MountPoint; v != nil {
		tfMap["mount_point"] = aws.ToString(v)
	}

	if v := apiObject.NumberOfDisks; v != nil {
		tfMap["number_of_disks"] = aws.ToInt32(v)
	}

	if v := apiObject.RaidLevel; v != nil {
		tfMap["raid_level"] = strconv.Itoa(int(aws.ToInt32(v)))
	}

	if v := apiObject.Size; v != nil {
		tfMap[names.AttrSize] = aws.ToInt32(v)
	}

	if v := apiObject.VolumeType; v != nil {
		tfMap[names.AttrType] = aws.ToString(v)
	}

	return tfMap
}

func flattenVolumeConfigurations(apiObjects []awstypes.VolumeConfiguration) []any {
	if len(apiObjects) == 0 {
		return nil
	}

	var tfList []any

	for _, apiObject := range apiObjects {
		tfList = append(tfList, flattenVolumeConfiguration(apiObject))
	}

	return tfList
}

func expandSetLoadBasedAutoScalingInput(tfMap map[string]any) *opsworks.SetLoadBasedAutoScalingInput {
	if tfMap == nil {
		return nil
	}

	apiObject := &opsworks.SetLoadBasedAutoScalingInput{}

	if v, ok := tfMap["downscaling"].([]any); ok && len(v) > 0 && v[0] != nil {
		apiObject.DownScaling = expandAutoScalingThresholds(v[0].(map[string]any))
	}

	if v, ok := tfMap["enable"].(bool); ok {
		apiObject.Enable = aws.Bool(v)
	}

	if v, ok := tfMap["upscaling"].([]any); ok && len(v) > 0 && v[0] != nil {
		apiObject.UpScaling = expandAutoScalingThresholds(v[0].(map[string]any))
	}

	return apiObject
}

func expandAutoScalingThresholds(tfMap map[string]any) *awstypes.AutoScalingThresholds {
	if tfMap == nil {
		return nil
	}

	apiObject := &awstypes.AutoScalingThresholds{}

	if v, ok := tfMap["alarms"].([]any); ok && len(v) > 0 {
		apiObject.Alarms = flex.ExpandStringValueList(v)
	}

	if v, ok := tfMap["cpu_threshold"].(float64); ok && v != 0.0 {
		apiObject.CpuThreshold = aws.Float64(v)
	}

	if v, ok := tfMap["ignore_metrics_time"].(int); ok && v != 0 {
		apiObject.IgnoreMetricsTime = aws.Int32(int32(v))
	}

	if v, ok := tfMap[names.AttrInstanceCount].(int); ok && v != 0 {
		apiObject.InstanceCount = aws.Int32(int32(v))
	}

	if v, ok := tfMap["load_threshold"].(float64); ok && v != 0.0 {
		apiObject.LoadThreshold = aws.Float64(v)
	}

	if v, ok := tfMap["memory_threshold"].(float64); ok && v != 0.0 {
		apiObject.MemoryThreshold = aws.Float64(v)
	}

	if v, ok := tfMap["thresholds_wait_time"].(int); ok && v != 0 {
		apiObject.ThresholdsWaitTime = aws.Int32(int32(v))
	}

	return apiObject
}

func flattenLoadBasedAutoScalingConfiguration(apiObject *awstypes.LoadBasedAutoScalingConfiguration) map[string]any {
	if apiObject == nil {
		return nil
	}

	tfMap := map[string]any{}

	if v := apiObject.DownScaling; v != nil {
		tfMap["downscaling"] = []any{flattenAutoScalingThresholds(v)}
	}

	if v := apiObject.Enable; v != nil {
		tfMap["enable"] = aws.ToBool(v)
	}

	if v := apiObject.UpScaling; v != nil {
		tfMap["upscaling"] = []any{flattenAutoScalingThresholds(v)}
	}

	return tfMap
}

func flattenAutoScalingThresholds(apiObject *awstypes.AutoScalingThresholds) map[string]any {
	if apiObject == nil {
		return nil
	}

	tfMap := map[string]any{}

	if v := apiObject.Alarms; v != nil {
		tfMap["alarms"] = v
	}

	if v := apiObject.CpuThreshold; v != nil {
		tfMap["cpu_threshold"] = aws.ToFloat64(v)
	}

	if v := apiObject.IgnoreMetricsTime; v != nil {
		tfMap["ignore_metrics_time"] = aws.ToInt32(v)
	}

	if v := apiObject.InstanceCount; v != nil {
		tfMap[names.AttrInstanceCount] = aws.ToInt32(v)
	}

	if v := apiObject.LoadThreshold; v != nil {
		tfMap["load_threshold"] = aws.ToFloat64(v)
	}

	if v := apiObject.MemoryThreshold; v != nil {
		tfMap["memory_threshold"] = aws.ToFloat64(v)
	}

	if v := apiObject.ThresholdsWaitTime; v != nil {
		tfMap["thresholds_wait_time"] = aws.ToInt32(v)
	}

	return tfMap
}
