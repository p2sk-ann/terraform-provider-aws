// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package redshift

//TODO: delete if not needed, if need, import
//	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
//	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
// "github.com/hashicorp/terraform-provider-aws/internal/conns"
// "github.com/hashicorp/terraform-provider-aws/internal/sweep"
// sweepfw "github.com/hashicorp/terraform-provider-aws/internal/sweep/framework"

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/redshift"
	awstypes "github.com/aws/aws-sdk-go-v2/service/redshift/types"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/retry"
	"github.com/hashicorp/terraform-provider-aws/internal/create"
	"github.com/hashicorp/terraform-provider-aws/internal/errs"
	"github.com/hashicorp/terraform-provider-aws/internal/errs/fwdiag"
	"github.com/hashicorp/terraform-provider-aws/internal/framework"
	"github.com/hashicorp/terraform-provider-aws/internal/framework/flex"
	fwtypes "github.com/hashicorp/terraform-provider-aws/internal/framework/types"
	tfslices "github.com/hashicorp/terraform-provider-aws/internal/slices"
	tftags "github.com/hashicorp/terraform-provider-aws/internal/tags"
	"github.com/hashicorp/terraform-provider-aws/internal/tfresource"
	"github.com/hashicorp/terraform-provider-aws/names"
)

// OK
// @FrameworkResource("aws_redshift_integration", name="Integration")
// @Tags(identifierAttribute="arn")
// @Testing(tagsTest=false)
func newResourceIntegration(_ context.Context) (resource.ResourceWithConfigure, error) {
	r := &resourceIntegration{}

	r.SetDefaultCreateTimeout(30 * time.Minute)
	r.SetDefaultUpdateTimeout(30 * time.Minute)
	r.SetDefaultDeleteTimeout(30 * time.Minute)

	return r, nil
}

// OK
const (
	integrationStatusActive         = "active"
	integrationStatusCreating       = "creating"
	integrationStatusDeleting       = "deleting"
	integrationStatusFailed         = "failed"
	integrationStatusModifying      = "modifying"
	integrationStatusNeedsAttention = "needs_attention"
	integrationStatusSyncing        = "syncing"
)

// OK
const (
	ResNameIntegration = "Integration"
)

// OK
type resourceIntegration struct {
	framework.ResourceWithConfigure
	framework.WithImportByID
	framework.WithTimeouts
}

// OK https://developer.hashicorp.com/terraform/plugin/framework/resources
func (*resourceIntegration) Metadata(_ context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
	response.TypeName = "aws_redshift_integration"
}

// OK
func (r *resourceIntegration) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"additional_encryption_context": schema.MapAttribute{
				CustomType:  fwtypes.MapOfStringType,
				ElementType: types.StringType, //TODO: check this is needed
				Optional:    true,
				PlanModifiers: []planmodifier.Map{
					mapplanmodifier.RequiresReplace(),
				},
			},
			names.AttrARN: framework.ARNAttributeComputedOnly(),
			names.AttrID:  framework.IDAttribute(),
			names.AttrDescription: schema.StringAttribute{
				Optional: true,
			},
			"integration_name": schema.StringAttribute{
				Required: true,
			},
			names.AttrKMSKeyID: schema.StringAttribute{
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"source_arn": schema.StringAttribute{
				CustomType: fwtypes.ARNType,
				Required:   true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			names.AttrTags:    tftags.TagsAttribute(),
			names.AttrTagsAll: tftags.TagsAttributeComputedOnly(),
			names.AttrTargetARN: schema.StringAttribute{
				CustomType: fwtypes.ARNType,
				Required:   true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
		},
		Blocks: map[string]schema.Block{
			names.AttrTimeouts: timeouts.Block(ctx, timeouts.Opts{
				Create: true,
				Update: true,
				Delete: true,
			}),
		},
	}
}

// OK
func (r *resourceIntegration) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	conn := r.Meta().RedshiftClient(ctx)

	var plan resourceIntegrationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var input redshift.CreateIntegrationInput
	// https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/redshift#CreateIntegrationInput
	resp.Diagnostics.Append(flex.Expand(ctx, plan, &input)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Additional fields.
	// https://github.com/hashicorp/terraform-provider-aws/blob/main/docs/resource-tagging.md
	// input.Tags = getTagsInV2(ctx) //TODO: survey later

	out, err := conn.CreateIntegration(ctx, &input)
	if err != nil {
		resp.Diagnostics.AddError(
			create.ProblemStandardMessage(names.Redshift, create.ErrActionCreating, ResNameIntegration, plan.IntegrationName.String(), err),
			err.Error(),
		)
		return
	}
	//TODO: delete if not needed
	if out == nil || out.IntegrationArn == nil {
		resp.Diagnostics.AddError(
			create.ProblemStandardMessage(names.Redshift, create.ErrActionCreating, ResNameIntegration, plan.IntegrationName.String(), nil),
			errors.New("empty output").Error(),
		)
		return
	}

	// Set values for unknowns.
	plan.IntegrationARN = flex.StringToFramework(ctx, out.IntegrationArn)
	plan.setID()

	// TOOD: modify検討
	prevAdditionalEncryptionContext := plan.AdditionalEncryptionContext

	resp.Diagnostics.Append(flex.Flatten(ctx, out, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// TODO: modify検討
	// Null vs. empty map handling.
	if prevAdditionalEncryptionContext.IsNull() && !plan.AdditionalEncryptionContext.IsNull() && len(plan.AdditionalEncryptionContext.Elements()) == 0 {
		plan.AdditionalEncryptionContext = prevAdditionalEncryptionContext
	}

	createTimeout := r.CreateTimeout(ctx, plan.Timeouts)
	integration, err := waitIntegrationCreated(ctx, conn, plan.ID.ValueString(), createTimeout)
	if err != nil {
		resp.State.SetAttribute(ctx, path.Root(names.AttrID), plan.ID) // Set 'id' so as to taint the resource.
		resp.Diagnostics.AddError(
			create.ProblemStandardMessage(names.Redshift, create.ErrActionWaitingForCreation, ResNameIntegration, plan.IntegrationName.String(), err),
			err.Error(),
		)
		return
	}

	// Set values for unknowns.
	plan.KMSKeyID = flex.StringToFramework(ctx, integration.KMSKeyId)

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// OK
func (r *resourceIntegration) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	conn := r.Meta().RedshiftClient(ctx)

	var state resourceIntegrationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := state.InitFromID(); err != nil {
		resp.Diagnostics.AddError("parsing resource ID", err.Error())

		return
	}

	out, err := findIntegrationByARN(ctx, conn, state.ID.ValueString())

	if tfresource.NotFound(err) {
		resp.Diagnostics.Append(fwdiag.NewResourceNotFoundWarningDiagnostic(err))
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError(
			create.ProblemStandardMessage(names.Redshift, create.ErrActionReading, ResNameIntegration, state.ID.String(), err),
			err.Error(),
		)
		return
	}

	prevAdditionalEncryptionContext := state.AdditionalEncryptionContext

	resp.Diagnostics.Append(flex.Flatten(ctx, out, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Null vs. empty map handling.
	if prevAdditionalEncryptionContext.IsNull() && !state.AdditionalEncryptionContext.IsNull() && len(state.AdditionalEncryptionContext.Elements()) == 0 {
		state.AdditionalEncryptionContext = prevAdditionalEncryptionContext
	}

	// setTagsOutV2(ctx, out.Tags) //TODO: survey later

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// OK
func (r *resourceIntegration) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	conn := r.Meta().RedshiftClient(ctx)

	var plan, state resourceIntegrationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	diff, d := flex.Diff(ctx, plan, state)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	if diff.HasChanges() {
		var input redshift.ModifyIntegrationInput
		resp.Diagnostics.Append(flex.Expand(ctx, plan, &input)...) //TODO: integration_name / descriptionだけ指定するように修正する必要あれば、そうする
		if resp.Diagnostics.HasError() {
			return
		}

		out, err := conn.ModifyIntegration(ctx, &input)
		if err != nil {
			resp.Diagnostics.AddError(
				create.ProblemStandardMessage(names.Redshift, create.ErrActionUpdating, ResNameIntegration, plan.ID.String(), err),
				err.Error(),
			)
			return
		}
		//TODO: delete if not needed
		if out == nil || out.IntegrationArn == nil {
			resp.Diagnostics.AddError(
				create.ProblemStandardMessage(names.Redshift, create.ErrActionUpdating, ResNameIntegration, plan.ID.String(), nil),
				errors.New("empty output").Error(),
			)
			return
		}

		resp.Diagnostics.Append(flex.Flatten(ctx, out, &plan)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	updateTimeout := r.UpdateTimeout(ctx, plan.Timeouts)
	_, err := waitIntegrationUpdated(ctx, conn, plan.ID.ValueString(), updateTimeout)
	if err != nil {
		resp.Diagnostics.AddError(
			create.ProblemStandardMessage(names.Redshift, create.ErrActionWaitingForUpdate, ResNameIntegration, plan.ID.String(), err),
			err.Error(),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// OK
func (r *resourceIntegration) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	conn := r.Meta().RedshiftClient(ctx)

	var state resourceIntegrationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	//TODO: if error, use "IntegrationIdentifier: aws.String(data.ID.ValueString())," instead.
	input := redshift.DeleteIntegrationInput{
		IntegrationArn: state.ID.ValueStringPointer(),
	}

	_, err := conn.DeleteIntegration(ctx, &input)
	if err != nil {
		if errs.IsA[*awstypes.IntegrationNotFoundFault](err) {
			return
		}

		resp.Diagnostics.AddError(
			create.ProblemStandardMessage(names.Redshift, create.ErrActionDeleting, ResNameIntegration, state.ID.String(), err),
			err.Error(),
		)
		return
	}

	deleteTimeout := r.DeleteTimeout(ctx, state.Timeouts)
	_, err = waitIntegrationDeleted(ctx, conn, state.ID.ValueString(), deleteTimeout)
	if err != nil {
		resp.Diagnostics.AddError(
			create.ProblemStandardMessage(names.Redshift, create.ErrActionWaitingForDeletion, ResNameIntegration, state.ID.String(), err),
			err.Error(),
		)
		return
	}
}

// TODO: delete if not needed
// OK
func (r *resourceIntegration) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root(names.AttrID), req, resp)
}

// OK TODO: move to wait.go
func waitIntegrationCreated(ctx context.Context, conn *redshift.Client, arn string, timeout time.Duration) (*awstypes.Integration, error) {
	stateConf := &retry.StateChangeConf{
		Pending:        []string{integrationStatusCreating, integrationStatusModifying},
		Target:         []string{integrationStatusActive}, //TODO: check if other statuses are needed like NeedsAttention/Syncing
		Refresh:        statusIntegration(ctx, conn, arn),
		Timeout:        timeout,
		NotFoundChecks: 20, //TODO: delete if not needed
	}

	outputRaw, err := stateConf.WaitForStateContext(ctx)
	if out, ok := outputRaw.(*awstypes.Integration); ok {
		tfresource.SetLastError(err, errors.Join(tfslices.ApplyToAll(out.Errors, integrationError)...))

		return out, err
	}

	return nil, err
}

// OK TODO: move to wait.go
func waitIntegrationUpdated(ctx context.Context, conn *redshift.Client, arn string, timeout time.Duration) (*awstypes.Integration, error) {
	stateConf := &retry.StateChangeConf{
		Pending:        []string{integrationStatusModifying},
		Target:         []string{integrationStatusActive}, //TODO: check if other statuses are needed like NeedsAttention/Syncing
		Refresh:        statusIntegration(ctx, conn, arn),
		Timeout:        timeout,
		NotFoundChecks: 20, //TODO: delete if not needed
	}

	outputRaw, err := stateConf.WaitForStateContext(ctx)
	if out, ok := outputRaw.(*awstypes.Integration); ok {
		tfresource.SetLastError(err, errors.Join(tfslices.ApplyToAll(out.Errors, integrationError)...))

		return out, err
	}

	return nil, err
}

// OK TODO: move to wait.go
func waitIntegrationDeleted(ctx context.Context, conn *redshift.Client, arn string, timeout time.Duration) (*awstypes.Integration, error) {
	stateConf := &retry.StateChangeConf{
		Pending: []string{integrationStatusDeleting, integrationStatusActive},
		Target:  []string{},
		Refresh: statusIntegration(ctx, conn, arn),
		Timeout: timeout,
	}

	outputRaw, err := stateConf.WaitForStateContext(ctx)
	if out, ok := outputRaw.(*awstypes.Integration); ok {
		tfresource.SetLastError(err, errors.Join(tfslices.ApplyToAll(out.Errors, integrationError)...))

		return out, err
	}

	return nil, err
}

// OK TODO: move to status.go
func statusIntegration(ctx context.Context, conn *redshift.Client, arn string) retry.StateRefreshFunc {
	return func() (any, string, error) {
		out, err := findIntegrationByARN(ctx, conn, arn)
		if tfresource.NotFound(err) {
			return nil, "", nil
		}

		if err != nil {
			return nil, "", err
		}

		// return out, aws.ToString(out.Status), nil //TODO: Delete later
		return out, string(out.Status), nil
	}
}

// OK TODO: move to find.go
func findIntegrationByARN(ctx context.Context, conn *redshift.Client, arn string) (*awstypes.Integration, error) {
	input := &redshift.DescribeIntegrationsInput{
		IntegrationArn: aws.String(arn),
	}

	return findIntegration(ctx, conn, input, tfslices.PredicateTrue[*awstypes.Integration]())
}

// OK TODO: move to find.go
func findIntegration(ctx context.Context, conn *redshift.Client, input *redshift.DescribeIntegrationsInput, filter tfslices.Predicate[*awstypes.Integration]) (*awstypes.Integration, error) {
	out, err := findIntegrations(ctx, conn, input, filter)

	if err != nil {
		return nil, err
	}

	//TODO: delete if not needed
	if out == nil || out[0].IntegrationArn == nil {
		return nil, tfresource.NewEmptyResultError(&input)
	}

	return tfresource.AssertSingleValueResult(out)
}

// OK TODO: move to find.go
func findIntegrations(ctx context.Context, conn *redshift.Client, input *redshift.DescribeIntegrationsInput, filter tfslices.Predicate[*awstypes.Integration]) ([]awstypes.Integration, error) {
	var out []awstypes.Integration

	pages := redshift.NewDescribeIntegrationsPaginator(conn, input)
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)

		if errs.IsA[*awstypes.IntegrationNotFoundFault](err) {
			return nil, &retry.NotFoundError{
				LastError:   err,
				LastRequest: input,
			}
		}

		if err != nil {
			return nil, err
		}

		for _, v := range page.Integrations {
			if filter(&v) {
				out = append(out, v)
			}
		}
	}

	return out, nil
}

// OK
func integrationError(v awstypes.IntegrationError) error {
	return fmt.Errorf("%s: %s", aws.ToString(v.ErrorCode), aws.ToString(v.ErrorMessage))
}

// OK
type resourceIntegrationModel struct {
	AdditionalEncryptionContext fwtypes.MapValueOf[types.String] `tfsdk:"additional_encryption_context"`
	Description                 types.String                     `tfsdk:"description"`
	ID                          types.String                     `tfsdk:"id"`
	IntegrationARN              types.String                     `tfsdk:"arn"`
	IntegrationName             types.String                     `tfsdk:"integration_name"`
	KMSKeyID                    types.String                     `tfsdk:"kms_key_id"`
	SourceARN                   fwtypes.ARN                      `tfsdk:"source_arn"`
	Tags                        tftags.Map                       `tfsdk:"tags"`
	TagsAll                     tftags.Map                       `tfsdk:"tags_all"`
	TargetARN                   fwtypes.ARN                      `tfsdk:"target_arn"`
	Timeouts                    timeouts.Value                   `tfsdk:"timeouts"`
}

// Once the sweeper function is implemented, register it in sweeper.go
// as follows:
//	awsv2.Register("aws_redshift_integration", sweepIntegrations)
// TODO: impl later //TODO: move it to sweep.go
// func sweepIntegrations(ctx context.Context, client *conns.AWSClient) ([]sweep.Sweepable, error) {
// 	input := redshift.ListIntegrationsInput{}
// 	conn := client.RedshiftClient(ctx)
// 	var sweepResources []sweep.Sweepable

// 	pages := redshift.NewListIntegrationsPaginator(conn, &input)
// 	for pages.HasMorePages() {
// 		page, err := pages.NextPage(ctx)
// 		if err != nil {
// 			return nil, err
// 		}

// 		for _, v := range page.Integrations {
// 			sweepResources = append(sweepResources, sweepfw.NewSweepResource(newResourceIntegration, client,
// 				sweepfw.NewAttribute(names.AttrID, aws.ToString(v.IntegrationId))),
// 			)
// 		}
// 	}

// 	return sweepResources, nil
// }

// OK
func (model *resourceIntegrationModel) InitFromID() error {
	model.IntegrationARN = model.ID

	return nil
}

// OK
func (model *resourceIntegrationModel) setID() {
	model.ID = model.IntegrationARN
}
