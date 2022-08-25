package ossaccesscontrol

import (
	"context"
	"errors"
	"strings"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/grafana/pkg/infra/log"
	"github.com/grafana/grafana/pkg/infra/metrics"
	"github.com/grafana/grafana/pkg/services/accesscontrol"
	"github.com/grafana/grafana/pkg/services/user"
	"github.com/grafana/grafana/pkg/setting"
)

var _ accesscontrol.AccessControl = new(AccessControl)

func ProvideAccessControl(cfg *setting.Cfg, service accesscontrol.Service) *AccessControl {
	logger := log.New("accesscontrol")
	return &AccessControl{
		cfg, logger, accesscontrol.NewResolvers(logger), service,
	}
}

type AccessControl struct {
	cfg       *setting.Cfg
	log       log.Logger
	resolvers accesscontrol.Resolvers
	service   accesscontrol.Service
}

func (a *AccessControl) Evaluate(ctx context.Context, user *user.SignedInUser, evaluator accesscontrol.Evaluator) (bool, error) {
	timer := prometheus.NewTimer(metrics.MAccessEvaluationsSummary)
	defer timer.ObserveDuration()
	metrics.MAccessEvaluationCount.Inc()

	if !verifyPermissions(user) {
		permissions, err := a.service.GetUserPermissions(ctx, user, accesscontrol.Options{ReloadCache: true})
		if err != nil {
			return false, err
		}
		user.Permissions = map[int64]map[string][]string{user.OrgID: accesscontrol.GroupScopesByAction(permissions)}
	}

	// Test evaluation without scope resolver first, this will prevent 403 for wildcard scopes when resource does not exist
	if evaluator.Evaluate(user.Permissions[user.OrgID]) {
		return true, nil
	}

	resolvedEvaluator, err := evaluator.MutateScopes(ctx, a.resolvers.GetScopeAttributeMutator(user.OrgID))
	if err != nil {
		if errors.Is(err, accesscontrol.ErrResolverNotFound) {
			return false, nil
		}
		return false, err
	}

	return resolvedEvaluator.Evaluate(user.Permissions[user.OrgID]), nil
}

type Checker func(scopes ...string) bool

func (a *AccessControl) GenerateChecker(ctx context.Context, user *user.SignedInUser, prefixes []string, action string) Checker {
	if !verifyPermissions(user) {
		return func(scope ...string) bool { return false }
	}

	permissions, ok := user.Permissions[user.OrgID]
	if !ok {
		return func(scope ...string) bool { return false }
	}

	checkers := map[string]Checker{}
	scopes, ok := permissions[action]
	if !ok {
		checkers[action] = func(scope ...string) bool { return false }
	}

	wildcards := generatePossibleWildcards(prefixes)
	lookup := make(map[string]bool, len(scopes)-1)
	for _, s := range scopes {
		if wildcards.Contains(s) {
			return func(scope ...string) bool { return true }
		}
		lookup[s] = true
	}

	return func(scopes ...string) bool {
		for _, s := range scopes {
			if lookup[s] {
				return true
			}
		}
		return false
	}
}

func (a *AccessControl) RegisterScopeAttributeResolver(prefix string, resolver accesscontrol.ScopeAttributeResolver) {
	a.resolvers.AddScopeAttributeResolver(prefix, resolver)
}

func (a *AccessControl) DeclareFixedRoles(registrations ...accesscontrol.RoleRegistration) error {
	// FIXME: Remove wrapped call
	return a.service.DeclareFixedRoles(registrations...)
}

func (a *AccessControl) IsDisabled() bool {
	return accesscontrol.IsDisabled(a.cfg)
}

func verifyPermissions(u *user.SignedInUser) bool {
	if u.Permissions == nil {
		return false
	}
	if _, ok := u.Permissions[u.OrgID]; !ok {
		return false
	}
	return true
}

type Wildcards []string

func (wildcards Wildcards) Contains(scope string) bool {
	for _, wildcard := range wildcards {
		if scope == wildcard {
			return true
		}
	}
	return false
}

func generatePossibleWildcards(prefixes []string) Wildcards {
	wildcards := Wildcards{"*"}
	for _, prefix := range prefixes {
		parts := strings.Split(prefix, ":")
		for _, p := range parts {
			wildcards = append(wildcards, p+":*")
		}
	}
	return wildcards
}
