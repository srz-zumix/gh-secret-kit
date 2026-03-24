package config

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/google/go-github/v84/github"
	"gopkg.in/yaml.v3"
)

// EnvironmentConfig holds exportable settings for a single GitHub Actions environment.
type EnvironmentConfig struct {
	Name                   string                      `yaml:"name" json:"name"`
	CanAdminsBypass        bool                        `yaml:"can_admins_bypass" json:"can_admins_bypass"`
	WaitTimer              int                         `yaml:"wait_timer,omitempty" json:"wait_timer,omitempty"`
	PreventSelfReview      bool                        `yaml:"prevent_self_review,omitempty" json:"prevent_self_review,omitempty"`
	Reviewers              []ReviewerConfig            `yaml:"reviewers,omitempty" json:"reviewers,omitempty"`
	DeploymentBranchPolicy *BranchPolicyConfig         `yaml:"deployment_branch_policy,omitempty" json:"deployment_branch_policy,omitempty"`
	BranchPolicies         []BranchPolicyPatternConfig `yaml:"branch_policies,omitempty" json:"branch_policies,omitempty"`
	Variables              []VariableConfig            `yaml:"variables,omitempty" json:"variables,omitempty"`
}

// ReviewerConfig holds a single reviewer entry (User or Team).
type ReviewerConfig struct {
	Type string `yaml:"type" json:"type"`
	Name string `yaml:"name" json:"name"`
}

// BranchPolicyConfig represents the deployment_branch_policy setting of an environment.
type BranchPolicyConfig struct {
	ProtectedBranches    bool `yaml:"protected_branches" json:"protected_branches"`
	CustomBranchPolicies bool `yaml:"custom_branch_policies" json:"custom_branch_policies"`
}

// BranchPolicyPatternConfig represents a single custom branch policy pattern.
type BranchPolicyPatternConfig struct {
	Name string `yaml:"name" json:"name"`
	Type string `yaml:"type,omitempty" json:"type,omitempty"`
}

// VariableConfig holds a single environment variable name/value pair.
type VariableConfig struct {
	Name  string `yaml:"name" json:"name"`
	Value string `yaml:"value" json:"value"`
}

// EnvironmentConfigFromGitHub builds an EnvironmentConfig from a github.Environment and its branch policies.
// policies should be the custom branch policies, or nil when not applicable.
func EnvironmentConfigFromGitHub(env *github.Environment, policies []*github.DeploymentBranchPolicy, vars []*github.ActionsVariable) *EnvironmentConfig {
	cfg := &EnvironmentConfig{
		Name:            ptrStr(env.Name),
		CanAdminsBypass: ptrBool(env.CanAdminsBypass),
	}

	if env.DeploymentBranchPolicy != nil {
		cfg.DeploymentBranchPolicy = &BranchPolicyConfig{
			ProtectedBranches:    ptrBool(env.DeploymentBranchPolicy.ProtectedBranches),
			CustomBranchPolicies: ptrBool(env.DeploymentBranchPolicy.CustomBranchPolicies),
		}
	}

	for _, rule := range env.ProtectionRules {
		if rule.Type == nil {
			continue
		}
		switch *rule.Type {
		case "wait_timer":
			if rule.WaitTimer != nil {
				cfg.WaitTimer = *rule.WaitTimer
			}
		case "required_reviewers":
			if rule.PreventSelfReview != nil {
				cfg.PreventSelfReview = *rule.PreventSelfReview
			}
			for _, rev := range rule.Reviewers {
				if rev.Type == nil {
					continue
				}
				reviewer := ReviewerConfig{Type: *rev.Type}
				switch v := rev.Reviewer.(type) {
				case *github.User:
					reviewer.Name = ptrStr(v.Login)
				case *github.Team:
					reviewer.Name = ptrStr(v.Slug)
				}
				if reviewer.Name != "" {
					cfg.Reviewers = append(cfg.Reviewers, reviewer)
				}
			}
		}
	}

	for _, p := range policies {
		cfg.BranchPolicies = append(cfg.BranchPolicies, BranchPolicyPatternConfig{
			Name: ptrStr(p.Name),
			Type: ptrStr(p.Type),
		})
	}

	for _, v := range vars {
		cfg.Variables = append(cfg.Variables, VariableConfig{
			Name:  v.Name,
			Value: v.Value,
		})
	}

	return cfg
}

// WriteEnvironmentConfigs writes a list of EnvironmentConfigs as YAML to w.
func WriteEnvironmentConfigs(cfgs []*EnvironmentConfig, w io.Writer) error {
	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)
	if err := enc.Encode(cfgs); err != nil {
		return fmt.Errorf("error encoding environment configs: %w", err)
	}
	return enc.Close()
}

// WriteEnvironmentConfigsToFile writes a list of EnvironmentConfigs as YAML to a file.
func WriteEnvironmentConfigsToFile(cfgs []*EnvironmentConfig, output string) (err error) {
	f, err := os.Create(output)
	if err != nil {
		return fmt.Errorf("error creating output file: %w", err)
	}
	defer func() {
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		} else if closeErr != nil {
			err = fmt.Errorf("write error: %w; error closing file: %v", err, closeErr)
		}
	}()
	return WriteEnvironmentConfigs(cfgs, f)
}

// ReadEnvironmentConfigs reads one or more EnvironmentConfigs from a file or stdin.
// Handles both single-object and array YAML formats.
func ReadEnvironmentConfigs(input string) (_ []*EnvironmentConfig, err error) {
	var r io.Reader
	if input == "-" {
		r = os.Stdin
	} else {
		f, openErr := os.Open(input)
		if openErr != nil {
			return nil, fmt.Errorf("error opening input file: %w", openErr)
		}
		defer func() {
			closeErr := f.Close()
			if err == nil {
				err = closeErr
			} else if closeErr != nil {
				err = fmt.Errorf("read error: %w; error closing file: %v", err, closeErr)
			}
		}()
		r = f
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("error reading input: %w", err)
	}

	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, fmt.Errorf("no environment configs found in input")
	}

	var cfgs []*EnvironmentConfig
	if err := yaml.Unmarshal(data, &cfgs); err == nil {
		if len(cfgs) == 0 {
			// Handle an empty YAML array as a valid "no configs" case
			return []*EnvironmentConfig{}, nil
		}
		if cfgs[0] != nil {
			return cfgs, nil
		}
	}

	var cfg EnvironmentConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("error parsing YAML input: %w", err)
	}
	return []*EnvironmentConfig{&cfg}, nil
}

// WriteFile writes the EnvironmentConfig to a file in YAML format.
func (c *EnvironmentConfig) WriteFile(output string) (err error) {
	f, err := os.Create(output)
	if err != nil {
		return fmt.Errorf("error creating output file: %w", err)
	}
	defer func() {
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		} else if closeErr != nil {
			err = fmt.Errorf("write error: %w; error closing file: %v", err, closeErr)
		}
	}()
	return c.Write(f)
}

// Write serialises the EnvironmentConfig as YAML to w.
func (c *EnvironmentConfig) Write(w io.Writer) error {
	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)
	if err := enc.Encode(c); err != nil {
		return fmt.Errorf("error encoding environment config: %w", err)
	}
	return enc.Close()
}

func ptrStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func ptrBool(b *bool) bool {
	if b == nil {
		return false
	}
	return *b
}
