package runtime

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	codexadapter "crew/internal/adapters/providers/codex"
	geminiadapter "crew/internal/adapters/providers/gemini"
	grokadapter "crew/internal/adapters/providers/grok"
	openaiadapter "crew/internal/adapters/providers/openai"
	"crew/internal/application"
)

type TextProviderConfig struct {
	BaseURL     string
	APIKey      string
	BinaryPath  string
	WorkingDir  string
	Timeout     time.Duration
	Temperature float64
	HTTPClient  *http.Client
}

type SandboxProviderConfig struct {
	BinaryPath      string
	Model           string
	SandboxRoot     string
	Timeout         time.Duration
	AdditionalWrite []string
}

type textProviderFactory func(cfg TextProviderConfig) (application.LLMProvider, error)
type sandboxProviderFactory func(cfg SandboxProviderConfig) (application.SandboxedAgentRuntime, error)

var textProviderFactories = map[string]textProviderFactory{
	"openai": func(cfg TextProviderConfig) (application.LLMProvider, error) {
		return openaiadapter.New(openaiadapter.Config{
			BaseURL:     cfg.BaseURL,
			APIKey:      cfg.APIKey,
			Timeout:     cfg.Timeout,
			Temperature: cfg.Temperature,
			HTTPClient:  cfg.HTTPClient,
		})
	},
	"gemini": func(cfg TextProviderConfig) (application.LLMProvider, error) {
		return geminiadapter.New(geminiadapter.Config{
			BaseURL:     cfg.BaseURL,
			APIKey:      cfg.APIKey,
			Timeout:     cfg.Timeout,
			Temperature: cfg.Temperature,
			HTTPClient:  cfg.HTTPClient,
		})
	},
	"grok": func(cfg TextProviderConfig) (application.LLMProvider, error) {
		return grokadapter.New(grokadapter.Config{
			BaseURL:     cfg.BaseURL,
			APIKey:      cfg.APIKey,
			Timeout:     cfg.Timeout,
			Temperature: cfg.Temperature,
			HTTPClient:  cfg.HTTPClient,
		})
	},
	"codex": func(cfg TextProviderConfig) (application.LLMProvider, error) {
		return codexadapter.NewText(codexadapter.TextConfig{
			BinaryPath:       cfg.BinaryPath,
			WorkingDirectory: cfg.WorkingDir,
			Timeout:          cfg.Timeout,
		})
	},
}

var sandboxProviderFactories = map[string]sandboxProviderFactory{
	"codex": func(cfg SandboxProviderConfig) (application.SandboxedAgentRuntime, error) {
		return codexadapter.New(codexadapter.Config{
			BinaryPath:      cfg.BinaryPath,
			Model:           cfg.Model,
			SandboxRoot:     cfg.SandboxRoot,
			Timeout:         cfg.Timeout,
			AdditionalWrite: append([]string(nil), cfg.AdditionalWrite...),
		})
	},
}

type routedLLMProvider struct {
	configs map[string]TextProviderConfig

	mu     sync.RWMutex
	cached map[string]application.LLMProvider
}

func newConfiguredLLMProvider(cfg Config) (application.LLMProvider, error) {
	return &routedLLMProvider{
		configs: cloneTextProviderConfigs(cfg.TextProviders),
		cached:  make(map[string]application.LLMProvider),
	}, nil
}

func (p *routedLLMProvider) Generate(ctx context.Context, request application.GenerationRequest) (application.GenerationResult, error) {
	providerName := strings.TrimSpace(request.Agent.Provider)
	if providerName == "" {
		return application.GenerationResult{}, fmt.Errorf("agent %q has no provider configured", request.Agent.ID)
	}
	if providerName == "local_stub" {
		return localStubLLMProvider{}.Generate(ctx, request)
	}

	provider, err := p.providerFor(providerName)
	if err != nil {
		return application.GenerationResult{}, err
	}
	return provider.Generate(ctx, request)
}

func (p *routedLLMProvider) providerFor(name string) (application.LLMProvider, error) {
	p.mu.RLock()
	if provider, ok := p.cached[name]; ok {
		p.mu.RUnlock()
		return provider, nil
	}
	p.mu.RUnlock()

	factory, ok := textProviderFactories[name]
	if !ok {
		return nil, fmt.Errorf("unsupported text provider %q", name)
	}

	provider, err := factory(p.configs[name])
	if err != nil {
		return nil, fmt.Errorf("configure text provider %q: %w", name, err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if existing, ok := p.cached[name]; ok {
		return existing, nil
	}
	p.cached[name] = provider
	return provider, nil
}

func cloneTextProviderConfigs(configs map[string]TextProviderConfig) map[string]TextProviderConfig {
	if len(configs) == 0 {
		return nil
	}

	cloned := make(map[string]TextProviderConfig, len(configs))
	for name, cfg := range configs {
		cloned[name] = cfg
	}
	return cloned
}

func sortedTextProviderNames(configs map[string]TextProviderConfig) []string {
	names := make([]string, 0, len(configs))
	for name := range configs {
		if strings.TrimSpace(name) == "" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

type routedSandboxRuntime struct {
	configs map[string]SandboxProviderConfig

	mu     sync.RWMutex
	cached map[string]application.SandboxedAgentRuntime
}

func newConfiguredSandboxRuntime(cfg Config) (application.SandboxedAgentRuntime, error) {
	if len(cfg.SandboxProviders) == 0 {
		return nil, nil
	}
	return &routedSandboxRuntime{
		configs: cloneSandboxProviderConfigs(cfg.SandboxProviders),
		cached:  make(map[string]application.SandboxedAgentRuntime),
	}, nil
}

func (r *routedSandboxRuntime) ExecuteTask(ctx context.Context, task application.SandboxTask) (application.SandboxTaskExecutionResult, error) {
	runtime, err := r.runtimeFor(task.RuntimeName)
	if err != nil {
		return application.SandboxTaskExecutionResult{}, err
	}
	return runtime.ExecuteTask(ctx, task)
}

func (r *routedSandboxRuntime) SupportsRuntime(name string) bool {
	_, err := r.runtimeFor(name)
	return err == nil
}

func (r *routedSandboxRuntime) ProviderClass() application.AgentProviderClass {
	return application.AgentProviderClassSandboxedRuntime
}

func (r *routedSandboxRuntime) runtimeFor(name string) (application.SandboxedAgentRuntime, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == "disabled" {
		return nil, fmt.Errorf("sandbox runtime %q is not enabled", name)
	}

	r.mu.RLock()
	if runtime, ok := r.cached[name]; ok {
		r.mu.RUnlock()
		return runtime, nil
	}
	r.mu.RUnlock()

	factory, ok := sandboxProviderFactories[name]
	if !ok {
		return nil, fmt.Errorf("unsupported sandbox provider %q", name)
	}

	cfg, ok := r.configs[name]
	if !ok {
		return nil, fmt.Errorf("sandbox provider %q is not configured", name)
	}

	runtime, err := factory(cfg)
	if err != nil {
		return nil, fmt.Errorf("configure sandbox provider %q: %w", name, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.cached[name]; ok {
		return existing, nil
	}
	r.cached[name] = runtime
	return runtime, nil
}

func cloneSandboxProviderConfigs(configs map[string]SandboxProviderConfig) map[string]SandboxProviderConfig {
	if len(configs) == 0 {
		return nil
	}

	cloned := make(map[string]SandboxProviderConfig, len(configs))
	for name, cfg := range configs {
		cloned[name] = SandboxProviderConfig{
			BinaryPath:      cfg.BinaryPath,
			Model:           cfg.Model,
			SandboxRoot:     cfg.SandboxRoot,
			Timeout:         cfg.Timeout,
			AdditionalWrite: append([]string(nil), cfg.AdditionalWrite...),
		}
	}
	return cloned
}

func sortedSandboxProviderNames(configs map[string]SandboxProviderConfig) []string {
	names := make([]string, 0, len(configs))
	for name := range configs {
		if strings.TrimSpace(name) == "" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
