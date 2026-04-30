package script

import (
	"encoding/json"
	"fmt"

	"github.com/takezoh/credproxy/credproxy"
)

// buildHookRequest encodes the hook stdin payload as a single JSON line.
func buildHookRequest(action, route string, req credproxy.Request) ([]byte, error) {
	hr := hookRequest{
		Action: action,
		Route:  route,
		Request: hookReqInfo{
			Method: req.Method,
			Path:   req.Path,
			Host:   req.Host,
		},
		Context: hookCtx{
			Client:      req.Metadata["client"],
			ProjectPath: req.Metadata["project_path"],
		},
	}
	b, err := json.Marshal(hr)
	if err != nil {
		return nil, fmt.Errorf("encode hook request: %w", err)
	}
	return b, nil
}
