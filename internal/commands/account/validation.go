package account

import (
	"errors"
	"fmt"
	"unicode"
	"unicode/utf8"
)

const (
	maxProfileNameLength = 64
	// maxAliasLength is the longest e-mail address RFC 5321 allows.
	maxAliasLength = 320
)

// validateProfileName keeps profile names shell- and YAML-friendly. Names are
// typed by hand and end up in a config file, so control characters and spaces
// are rejected rather than escaped.
func validateProfileName(name string) error {
	if name == "" {
		return errors.New("cannot be empty")
	}
	if utf8.RuneCountInString(name) > maxProfileNameLength {
		return fmt.Errorf("must be %d characters or fewer", maxProfileNameLength)
	}
	for _, character := range name {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			continue
		}
		switch character {
		case '-', '_', '.':
			continue
		default:
			return fmt.Errorf("%q is not allowed; use letters, numbers, dots, dashes, or underscores", character)
		}
	}
	return nil
}

// validateShopifyAlias sanity-checks what the Shopify CLI reported back, which
// is parsed out of its terminal output and therefore never trusted blindly.
func validateShopifyAlias(alias string) error {
	if alias == "" {
		return errors.New("shopify account cannot be empty")
	}
	if utf8.RuneCountInString(alias) > maxAliasLength {
		return errors.New("shopify account is too long")
	}
	for _, character := range alias {
		if unicode.IsControl(character) {
			return errors.New("shopify account contains control characters")
		}
	}
	return nil
}
