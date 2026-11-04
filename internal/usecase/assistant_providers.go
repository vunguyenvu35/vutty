package usecase

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
	"vutty/internal/domain/assistant"
)

// ---- providers ----

type ProviderView struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Status      assistant.Status `json:"status"`
	Enabled     bool             `json:"enabled"`
	Default     bool             `json:"default"`
	Model       string           `json:"model"`
	SupportsMCP bool             `json:"supports_mcp"`
	// SupportsImages: the provider takes pictures (they are sent as they are, after the user confirms).
	SupportsImages bool `json:"supports_images"`
	Custom         bool `json:"custom"`
}

func (a *AssistantService) agents() []Agent {
	list := append([]Agent{}, a.c.Agents...)
	if a.c.NewCustomAgent != nil {
		for _, cp := range a.config().Custom {
			list = append(list, a.c.NewCustomAgent(cp))
		}
	}
	return list
}

func (a *AssistantService) agent(id string) (Agent, bool) {
	for _, ag := range a.agents() {
		if ag.ID() == id {
			return ag, true
		}
	}
	return nil, false
}

func (a *AssistantService) detectOne(ag Agent) assistant.Status {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	st := ag.Detect(ctx)
	a.mu.Lock()
	a.status[ag.ID()] = st
	a.mu.Unlock()
	return st
}

func (a *AssistantService) detectAll() {
	var wg sync.WaitGroup
	for _, ag := range a.agents() {
		wg.Add(1)
		go func(ag Agent) { defer wg.Done(); a.detectOne(ag) }(ag)
	}
	wg.Wait()
	a.mu.Lock()
	a.detected = true
	a.mu.Unlock()
}

func (a *AssistantService) providerViews() []ProviderView {
	cfg := a.config()
	custom := map[string]bool{}
	for _, cp := range cfg.Custom {
		custom[cp.ID] = true
	}
	agents := a.agents() // before the lock: it reads the configuration
	a.mu.Lock()
	defer a.mu.Unlock()
	out := []ProviderView{}
	for _, ag := range agents {
		pc := cfg.Providers[ag.ID()]
		ia, _ := ag.(ImageAgent)
		out = append(out, ProviderView{ID: ag.ID(), Name: ag.Name(), Status: a.status[ag.ID()], Enabled: pc.Enabled,
			Default: cfg.DefaultProvider == ag.ID(), Model: pc.Model, SupportsMCP: ag.SupportsMCP(), SupportsImages: ia != nil && ia.SupportsImages(), Custom: custom[ag.ID()]})
	}
	return out
}

// Warm starts detection in the background (once), so the answer is ready by the time a page asks for it. It
// starts no prompt and returns at once.
func (a *AssistantService) Warm() {
	a.mu.Lock()
	if a.detected || a.detecting {
		a.mu.Unlock()
		return
	}
	a.detecting = true
	a.mu.Unlock()
	go func() {
		a.detectAll()
		a.mu.Lock()
		a.detecting = false
		a.mu.Unlock()
		a.c.emit("assistant:providers")
	}()
}

// Providers lists the providers at once. Detection (starting each CLI to ask its version and sign-in) can take a few
// seconds, so it runs in the background: a provider that has not been looked at yet has an empty state, and the
// caller asks again a moment later (or hears assistant:providers).
func (a *AssistantService) Providers() []ProviderView {
	a.Warm()
	return a.providerViews()
}

// RefreshProviders runs detection again (the Refresh button).
func (a *AssistantService) RefreshProviders() []ProviderView {
	a.detectAll()
	return a.providerViews()
}

// SetProviderEnabled switches a provider on or off. Turning one on needs it to be ready and to pass the
// isolation self-test, which asks it to use a tool and must fail to do so.
func (a *AssistantService) SetProviderEnabled(id string, on bool) error {
	ag, ok := a.agent(id)
	if !ok {
		return errors.New("unknown provider")
	}
	if !on {
		return a.update(func(c *assistant.Config) error {
			pc := c.Providers[id]
			pc.Enabled = false
			c.Providers[id] = pc
			if c.DefaultProvider == id {
				c.DefaultProvider = ""
				for other, p := range c.Providers {
					if p.Enabled {
						c.DefaultProvider = other
						break
					}
				}
			}
			return nil
		})
	}
	st := a.detectOne(ag)
	if st.State != assistant.StateReady {
		msg := string(st.State)
		if st.Detail != "" {
			msg = st.Detail
		}
		if st.Hint != "" {
			msg += " (" + st.Hint + ")"
		}
		return fmt.Errorf("%s is not ready: %s", ag.Name(), msg)
	}
	if pc := a.config().Providers[id]; pc.VerifiedVersion == "" || pc.VerifiedVersion != st.Version {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := ag.Verify(ctx); err != nil {
			return fmt.Errorf("%s was not enabled: %w", ag.Name(), err)
		}
	}
	return a.update(func(c *assistant.Config) error {
		pc := c.Providers[id]
		pc.Enabled, pc.VerifiedVersion = true, st.Version
		c.Providers[id] = pc
		if c.DefaultProvider == "" {
			c.DefaultProvider = id
		}
		return nil
	})
}

// SetDefaultProvider chooses the provider the panel starts with.
func (a *AssistantService) SetDefaultProvider(id string) error {
	return a.update(func(c *assistant.Config) error {
		if !c.Providers[id].Enabled {
			return errors.New("enable the provider first")
		}
		c.DefaultProvider = id
		return nil
	})
}

// SetProviderModel stores a provider's default model ("" = the CLI decides). The model is chosen from the list the
// provider itself gave: an id it did not list is refused, so nothing typed by hand can reach a command line.
func (a *AssistantService) SetProviderModel(id, model string) error {
	model = strings.TrimSpace(model)
	if _, ok := a.agent(id); !ok {
		return errors.New("unknown provider")
	}
	if model != "" {
		list, err := a.Models(id, false)
		if err != nil {
			return err
		}
		listed := false
		for _, m := range list.Models {
			listed = listed || m.ID == model
		}
		if !listed || !modelID.MatchString(model) {
			return errors.New("choose a model from the list")
		}
	}
	return a.update(func(c *assistant.Config) error {
		pc := c.Providers[id]
		pc.Model = model
		c.Providers[id] = pc
		return nil
	})
}

// ModelList is what a provider offers. Default comes first and passes no model flag; the rest is read from
// the provider itself. A provider that cannot list its models returns only the first: nothing is invented.
type ModelList struct {
	Models []assistant.Model `json:"models"`
	Error  string            `json:"error"`
	// CanList is false when the provider has no way to list models (only the default is offered).
	CanList bool `json:"can_list"`
}

// Models returns the model list of a provider, loading it from the provider when the cache is old.
func (a *AssistantService) Models(id string, refresh bool) (ModelList, error) {
	ag, ok := a.agent(id)
	if !ok {
		return ModelList{}, errors.New("unknown provider")
	}
	// one load per provider at a time: the UI may ask from several places at once, and every load starts the CLI
	a.mu.Lock()
	fl := a.flights[id]
	if fl == nil {
		fl = &sync.Mutex{}
		a.flights[id] = fl
	}
	a.mu.Unlock()
	fl.Lock()
	defer fl.Unlock()

	a.mu.Lock()
	mc, have := a.models[id]
	a.mu.Unlock()
	age := time.Since(mc.at)
	fresh := have && mc.ok && age <= modelsTTL
	reload := !fresh
	if have && !mc.ok && age < modelsRetryAfter && !refresh {
		reload = false // it just failed: show that instead of starting the CLI again at once
	}
	if refresh {
		reload = true // an explicit Refresh always asks the provider
	}
	if reload {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		list, err := ag.Models(ctx)
		cancel()
		switch {
		case err != nil && have && mc.ok:
			mc.err = "Could not refresh models: " + err.Error() // keep the previous list
		case err != nil:
			mc = modelCache{err: err.Error(), at: time.Now()}
		default:
			mc = modelCache{list: list, ok: true, at: time.Now()}
		}
		a.mu.Lock()
		a.models[id] = mc
		a.mu.Unlock()
	}
	out := ModelList{Error: mc.err, CanList: len(mc.list) > 0 || mc.err != ""}
	out.Models = append(out.Models, assistant.Model{ID: "", Name: "Default (CLI decides)"})
	seen := map[string]bool{"": true}
	for _, m := range mc.list {
		if !seen[m.ID] {
			seen[m.ID] = true
			out.Models = append(out.Models, m)
		}
	}
	return out, nil
}

// SaveCustomProvider adds or changes a user-defined provider. It stays off until it passes the self-test.
func (a *AssistantService) SaveCustomProvider(p assistant.CustomProvider) error {
	p.Name, p.Command = strings.TrimSpace(p.Name), strings.TrimSpace(p.Command)
	if p.Name == "" || p.Command == "" {
		return errors.New("name and command are required")
	}
	if p.PromptVia != "" && p.PromptVia != "stdin" && p.PromptVia != "arg" {
		return errors.New("prompt must be passed by stdin or arg")
	}
	if p.ID == "" {
		slug := regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(strings.ToLower(p.Name), "-")
		p.ID = "custom-" + strings.Trim(slug, "-")
	}
	if !strings.HasPrefix(p.ID, "custom-") {
		return errors.New("invalid provider id")
	}
	err := a.update(func(c *assistant.Config) error {
		for i, x := range c.Custom {
			if x.ID == p.ID {
				c.Custom[i] = p
				pc := c.Providers[p.ID] // a changed command must be verified again
				pc.Enabled, pc.VerifiedVersion = false, ""
				c.Providers[p.ID] = pc
				return nil
			}
		}
		c.Custom = append(c.Custom, p)
		return nil
	})
	if err == nil {
		a.detectAll()
	}
	return err
}

func (a *AssistantService) DeleteCustomProvider(id string) error {
	return a.update(func(c *assistant.Config) error {
		var keep []assistant.CustomProvider
		for _, x := range c.Custom {
			if x.ID != id {
				keep = append(keep, x)
			}
		}
		c.Custom = keep
		delete(c.Providers, id)
		if c.DefaultProvider == id {
			c.DefaultProvider = ""
		}
		return nil
	})
}
