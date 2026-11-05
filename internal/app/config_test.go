package app

import (
	"strings"
	"testing"
)

func validConfig(t *testing.T) Config {
	t.Helper()
	cfg, err := LoadConfig("", "../../config/config.yaml")
	if err != nil {
		t.Fatalf("default config.yaml must be valid: %v", err)
	}
	return cfg
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(c *Config)
		section string // empty = valid
	}{
		{"defaults", func(*Config) {}, ""},
		{"no service name", func(c *Config) { c.Service.Name = "" }, "service"},
		{"bad environment", func(c *Config) { c.Service.Environment = "prod" }, "service"},
		{"no addr", func(c *Config) { c.HTTP.Addr = "" }, "http"},
		{"no body limit", func(c *Config) { c.HTTP.MaxBodyBytes = 0 }, "http"},
		{"no db host", func(c *Config) { c.Database.Leader.Host = "" }, "database"},
		{"metrics on needs path", func(c *Config) { c.Metrics.Enabled = true; c.Metrics.Path = "" }, "metrics"},
		{"metrics off ignores path", func(c *Config) { c.Metrics.Enabled = false; c.Metrics.Path = "" }, ""},
		{"swagger on needs credentials", func(c *Config) { c.Swagger.Enabled = true }, "swagger"},
		{"swagger on with credentials", func(c *Config) {
			c.Swagger = SwaggerConfig{Enabled: true, Username: "u", Password: "p"}
		}, ""},
		{"tracing on needs endpoint", func(c *Config) { c.Tracing.Enabled = true; c.Tracing.Endpoint = "" }, "tracing"},
		{"production needs auth", func(c *Config) { c.Service.Environment = EnvProduction }, "auth"},
		{"production with auth on", func(c *Config) { c.Service.Environment = EnvProduction; c.Auth.Enabled = true }, ""},
		{"auth on needs jwks_url", func(c *Config) { c.Auth.Enabled = true; c.Auth.Local.JWKSURL = "" }, "auth"},
		{"bad authz source", func(c *Config) { c.Authz.Source = "ldap" }, "authz"},
		{"identity source needs client secret", func(c *Config) {
			c.Auth.Enabled = true
			c.Authz.Source = "identity"
			c.Identity.ClientSecret = ""
		}, "identity"},
		{"identity source with auth off needs no secret", func(c *Config) {
			c.Authz.Source = "identity"
			c.Identity.ClientSecret = ""
		}, ""},
		{"note list size over 100", func(c *Config) { c.Modules.Note.ListMaxSize = 101 }, "modules.note"},
		{"bad tenancy mode", func(c *Config) { c.Tenancy.Mode = "shared" }, "tenancy"},
		{"negative drain delay", func(c *Config) { c.Lifecycle.DrainDelay = -1 }, "lifecycle"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig(t)
			tt.mutate(&cfg)
			err := cfg.Validate()
			if tt.section == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "config "+tt.section+":") {
				t.Fatalf("Validate() = %v, want an error naming section %q", err, tt.section)
			}
		})
	}
}

func TestLoadConfigDerivesDefaults(t *testing.T) {
	cfg := validConfig(t)
	if cfg.Tracing.ServiceName != cfg.Service.Name || cfg.Validator.FieldNameTag != "json" {
		t.Errorf("derived defaults not applied: %+v / %+v", cfg.Tracing.Config, cfg.Validator)
	}
	if cfg.Authz.Source != "config" || len(cfg.Authz.Roles[devRole]) != 1 || cfg.Authz.Roles[devRole][0] != "*" {
		t.Errorf("auth off should force authz to source config with %s: [*], got %+v", devRole, cfg.Authz)
	}
}
