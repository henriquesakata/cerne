package app

import "regexp"

// What must not reach the shared backend as free text: an e-mail or the id of
// a person. Shared by the runbook record and the work log (F-0397).
var (
	// emailRe: the first label after @ holds a letter and the TLD is alphabetic,
	// so next@15.0.1, pkg@1.2.3 and ./build@2.0.log (versions) are not e-mails.
	emailRe = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9-]*[A-Za-z][A-Za-z0-9-]*(?:\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}`)
	// personIDRe: a UUID or a CPF, the ids of a person that must not reach the
	// backend. Limit: an employee/registration number ("matricula 12345") has no
	// format to tell it from a ticket or a pid, so it is not refused.
	personIDRe = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b|\b\d{3}\.\d{3}\.\d{3}-\d{2}\b`)
)
