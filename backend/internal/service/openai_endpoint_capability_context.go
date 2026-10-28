package service

import "context"

// openAIRequiredEndpointCapabilityKey carries a request-scoped upstream
// capability requirement into the legacy mixed-platform scheduler. Most
// requests can use the default capability, but Claude Messages requests that
// contain tools for GPT models must be scheduled only to Responses-capable
// OpenAI API-key accounts.
type openAIRequiredEndpointCapabilityKey struct{}

// WithOpenAIRequiredEndpointCapability marks a request as requiring a
// particular OpenAI-compatible upstream endpoint capability.
func WithOpenAIRequiredEndpointCapability(ctx context.Context, capability OpenAIEndpointCapability) context.Context {
	if ctx == nil || capability == "" {
		return ctx
	}
	return context.WithValue(ctx, openAIRequiredEndpointCapabilityKey{}, capability)
}

// OpenAIRequiredEndpointCapabilityFromContext returns the request-scoped
// capability requirement, if one was set.
func OpenAIRequiredEndpointCapabilityFromContext(ctx context.Context) OpenAIEndpointCapability {
	if ctx == nil {
		return ""
	}
	capability, _ := ctx.Value(openAIRequiredEndpointCapabilityKey{}).(OpenAIEndpointCapability)
	return capability
}
