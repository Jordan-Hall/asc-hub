// SPDX-License-Identifier: MIT

package templates

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPasswordPolicyOf(t *testing.T) {
	assert.Equal(t, PasswordPolicy{MinLength: "8"}, passwordPolicyOf(8, []string{"off"}))
	assert.Equal(t, PasswordPolicy{MinLength: "1"}, passwordPolicyOf(0, nil))
	assert.Equal(t, PasswordPolicy{MinLength: "12", Classes: "lower,upper,digit"}, passwordPolicyOf(12, []string{"digit", "upper", "lower"}))
	assert.Equal(t, PasswordPolicy{MinLength: "8", Classes: "lower,upper,digit,spec"}, passwordPolicyOf(8, []string{"bogus"}))
}
