package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/takezoh/credproxy/credproxy"
	"github.com/takezoh/credproxy/providers/onepassword"
)

type onePasswordRoute struct {
	store      *onepassword.Provider
	credential string
	delivery   string
	header     string
	prefix     string
	env        map[string]string
}

func (r *onePasswordRoute) Get(ctx context.Context, _ credproxy.Request) (*credproxy.Injection, error) {
	return r.inject(ctx, false)
}
func (r *onePasswordRoute) Refresh(ctx context.Context, _ credproxy.Request) (*credproxy.Injection, error) {
	return r.inject(ctx, true)
}

func (r *onePasswordRoute) inject(ctx context.Context, refresh bool) (*credproxy.Injection, error) {
	read := r.store.Get
	if refresh {
		read = r.store.Refresh
	}
	if r.delivery == "header" {
		value, err := read(ctx, r.credential)
		if err != nil {
			return nil, err
		}
		return &credproxy.Injection{Headers: map[string]string{r.header: r.prefix + value}}, nil
	}
	values := make(map[string]string, len(r.env))
	for key, name := range r.env {
		value, err := read(ctx, name)
		if err != nil {
			return nil, err
		}
		values[key] = value
	}
	body, err := json.Marshal(struct {
		Env map[string]string `json:"env"`
	}{values})
	if err != nil {
		return nil, fmt.Errorf("encode env route: %w", err)
	}
	return &credproxy.Injection{BodyReplace: body}, nil
}

type adminRefresher struct{ store *onepassword.Provider }

func (a adminRefresher) Refresh(ctx context.Context, names []string, allPreloaded bool) error {
	return a.store.RefreshMany(ctx, names, allPreloaded)
}
