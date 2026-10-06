package conformance

import "github.com/skosovsky/memy"

const foreignScopeSuffix = "-foreign"
const conformanceOther = "other"
const visibilityUpgradeRecord = "upgrade"

func foreignScopes(original memy.Scope) []memy.Scope {
	tenant, namespace, subject := original, original, original
	tenant.Tenant += foreignScopeSuffix
	namespace.Namespace += foreignScopeSuffix
	subject.Subject += foreignScopeSuffix
	return []memy.Scope{tenant, namespace, subject}
}
