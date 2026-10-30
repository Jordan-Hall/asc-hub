// SPDX-License-Identifier: MIT

package templates

import (
	"slices"
	"strconv"
	"strings"

	"forgejo.org/modules/setting"
)

// PasswordPolicy is what the account pages tell a person about the password rules, so the strength meter can show them
// before the form is sent. The server still enforces the rules; this only describes them.
type PasswordPolicy struct {
	MinLength string // as text, for a data attribute
	Classes   string // required character classes, comma separated: lower, upper, digit, spec; empty when none are required
}

var passwordClassOrder = []string{"lower", "upper", "digit", "spec"}

func passwordPolicy() PasswordPolicy {
	return passwordPolicyOf(setting.MinPasswordLength, setting.PasswordComplexity)
}

func passwordPolicyOf(minLength int, complexity []string) PasswordPolicy {
	policy := PasswordPolicy{MinLength: strconv.Itoa(max(minLength, 1))}
	if len(complexity) == 1 && complexity[0] == "off" {
		return policy
	}
	var classes []string
	for _, class := range passwordClassOrder {
		if slices.Contains(complexity, class) {
			classes = append(classes, class)
		}
	}
	if len(classes) == 0 && len(complexity) > 0 {
		// like the server: no known class named means every class is required
		classes = passwordClassOrder
	}
	policy.Classes = strings.Join(classes, ",")
	return policy
}
