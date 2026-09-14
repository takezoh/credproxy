package hostbrokeradapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

type registerResponse struct {
	InstanceID string `json:"instanceId"`
	Generation string `json:"generation"`
}

// MaintainRegistration registers the live endpoint and renews its finite lease.
func MaintainRegistration(ctx context.Context, cfg Config) error {
	token, err := readSecret(cfg.Registration.TokenFile)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	registered, err := callRegistration[registerResponse](ctx, client, cfg.Registration.BrokerURL,
		"RegisterInstance", token, map[string]any{
			"service": cfg.Registration.Service, "contractVersion": cfg.Registration.ContractVersion,
			"endpoint": cfg.Registration.EndpointRef,
		})
	if err != nil {
		return fmt.Errorf("credproxy adapter registration failed: %w", err)
	}
	generation, parseErr := strconv.ParseUint(registered.Generation, 10, 64)
	if parseErr != nil || registered.InstanceID == "" || generation == 0 {
		return errors.New("credproxy adapter registration response is invalid")
	}
	ticker := time.NewTicker(time.Duration(cfg.Registration.RenewIntervalSecond) * time.Second)
	defer ticker.Stop()
	defer callRegistration[map[string]any](context.Background(), client, cfg.Registration.BrokerURL,
		"UnregisterInstance", token, map[string]any{"instanceId": registered.InstanceID, "generation": registered.Generation})
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if _, err := callRegistration[map[string]any](ctx, client, cfg.Registration.BrokerURL,
				"RenewLease", token, map[string]any{"instanceId": registered.InstanceID, "generation": registered.Generation}); err != nil {
				return fmt.Errorf("credproxy adapter lease renewal failed: %w", err)
			}
		}
	}
}

func callRegistration[T any](ctx context.Context, client *http.Client, baseURL, method, token string, input any) (T, error) {
	var zero T
	raw, err := json.Marshal(input)
	if err != nil {
		return zero, err
	}
	endpoint := baseURL + "/hostbroker.v1.RegistrationService/" + method
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return zero, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := client.Do(request)
	if err != nil {
		return zero, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return zero, fmt.Errorf("registration HTTP status %d", response.StatusCode)
	}
	var output T
	decoder := json.NewDecoder(response.Body)
	if err := decoder.Decode(&output); err != nil {
		return zero, err
	}
	return output, nil
}
