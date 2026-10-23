package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// RefreshOpenAIReferral queries the invitation eligibility and makes Sub2API
// persist the referral snapshot, so the next directory read reflects it.
func (s *sub2apiRelay) RefreshOpenAIReferral(ctx context.Context, accountID int64) (*OpenAIReferral, error) {
	if accountID <= 0 {
		return nil, fmt.Errorf("relay: refresh OpenAI referral: account ID must be positive")
	}
	operation := fmt.Sprintf("relay: refresh OpenAI referral for account %d", accountID)
	resp, err := s.doAdminRequest(ctx, http.MethodPost, fmt.Sprintf("/api/v1/admin/openai/accounts/%d/referrals/refresh", accountID), nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", operation, err)
	}
	data, err := readOwnerAccountData(operation, resp, true)
	if err != nil {
		return nil, err
	}
	referral, err := decodeOwnerAccountReferralEligibility(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", operation, err)
	}
	return referral, nil
}

// SendOpenAIReferralInvite sends exactly one invitation. Any answer that does not
// prove the invitation was sent is reported as an unknown outcome: Sub2API itself
// answers 502 when it cannot tell, and retrying a possibly sent invitation would
// risk a duplicate.
func (s *sub2apiRelay) SendOpenAIReferralInvite(ctx context.Context, accountID int64, request OpenAIReferralInviteRequest) (*OpenAIReferralInviteResult, error) {
	if accountID <= 0 {
		return nil, fmt.Errorf("relay: send OpenAI referral invite: account ID must be positive")
	}
	operation := fmt.Sprintf("relay: send OpenAI referral invite for account %d", accountID)
	payload, err := json.Marshal(map[string]any{
		"email":      strings.TrimSpace(request.Email),
		"program_id": strings.TrimSpace(request.ProgramID),
		"confirmed":  request.Confirmed,
	})
	if err != nil {
		return nil, fmt.Errorf("%s: marshal: %w", operation, err)
	}
	resp, err := s.doAdminRequest(ctx, http.MethodPost, fmt.Sprintf("/api/v1/admin/openai/accounts/%d/referrals/invite", accountID), bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", operation, err)
	}
	data, err := mutateOwnerAccountData(operation, resp)
	if err != nil {
		return nil, err
	}
	var response struct {
		Email         string                   `json:"email"`
		Sent          *bool                    `json:"sent"`
		Eligibility   *sub2apiReferralSnapshot `json:"eligibility"`
		RefreshFailed bool                     `json:"refresh_failed"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("%s: decode: %w", operation, err)
	}
	if response.Sent == nil || !*response.Sent {
		return nil, fmt.Errorf("%w: %s was not confirmed as sent", ErrUpstreamOutcomeUnknown, operation)
	}
	result := &OpenAIReferralInviteResult{Email: strings.TrimSpace(response.Email)}
	if response.Eligibility != nil {
		result.Referral = projectOwnerAccountReferral(*response.Eligibility)
	}
	return result, nil
}

func decodeOwnerAccountReferralEligibility(data json.RawMessage) (*OpenAIReferral, error) {
	var payload struct {
		Eligibility *sub2apiReferralSnapshot `json:"eligibility"`
	}
	if len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, &payload); err != nil {
			return nil, fmt.Errorf("decode eligibility: %w", err)
		}
	}
	if payload.Eligibility == nil {
		// An empty eligibility is not an eligibility observation; reporting it as
		// one would let the console offer an invitation from nothing.
		return nil, fmt.Errorf("decode eligibility: empty eligibility")
	}
	return projectOwnerAccountReferral(*payload.Eligibility), nil
}
