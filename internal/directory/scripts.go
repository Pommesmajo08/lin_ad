package directory

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"unicode/utf8"
)

const MaxJobScript = 16 * 1024

func NormalizeJobScript(interpreter, source string) (string, string, string, error) {
	if interpreter == "" {
		interpreter = "bash"
	}
	if interpreter != "sh" && interpreter != "bash" && interpreter != "python3" {
		return "", "", "", errors.New("interpreter must be sh, bash or python3")
	}
	source = strings.TrimPrefix(strings.ReplaceAll(source, "\r\n", "\n"), "\ufeff")
	if len(source) > MaxJobScript || strings.TrimSpace(source) == "" || !utf8.ValidString(source) || strings.ContainsRune(source, 0) {
		return "", "", "", errors.New("script must contain valid UTF-8 text without NUL, between 1 and 16384 bytes")
	}
	sum := sha256.Sum256([]byte(source))
	return interpreter, source, hex.EncodeToString(sum[:]), nil
}
