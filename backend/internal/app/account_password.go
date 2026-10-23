package app

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

const accountPasswordIterations = 600000

func normalizeAccountUsername(name string) (string, string, error) {
	name = strings.TrimSpace(name)
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) < 2 || utf8.RuneCountInString(name) > 32 {
		return "", "", errors.New("用户名需为 2–32 个字，支持中文、字母、数字、下划线和短横线")
	}
	for _, char := range name {
		if !unicode.IsLetter(char) && !unicode.IsDigit(char) && char != '_' && char != '-' {
			return "", "", errors.New("用户名只能包含中文、字母、数字、下划线和短横线")
		}
	}
	return strings.ToLower(name), name, nil
}

func validateAccountPassword(password string) error {
	count := utf8.RuneCountInString(password)
	if !utf8.ValidString(password) || count < 10 || count > 128 {
		return errors.New("密码需为 10–128 个字符，可使用长短语")
	}
	return nil
}

func accountPasswordKey(password string, salt []byte, iterations int) []byte {
	mac := hmac.New(sha256.New, []byte(password))
	mac.Write(salt)
	mac.Write([]byte{0, 0, 0, 1})
	block := mac.Sum(nil)
	key := append([]byte(nil), block...)
	for remaining := iterations - 1; remaining > 0; remaining-- {
		mac.Reset()
		mac.Write(block)
		block = mac.Sum(block[:0])
		for index, value := range block {
			key[index] ^= value
		}
	}
	return key
}

func newAccountPassword(password string) (string, string, error) {
	if err := validateAccountPassword(password); err != nil {
		return "", "", err
	}
	salt := randomHex(16)
	if salt == "" {
		return "", "", errors.New("无法生成密码盐，请重试")
	}
	decoded, _ := hex.DecodeString(salt)
	return salt, hex.EncodeToString(accountPasswordKey(password, decoded, accountPasswordIterations)), nil
}

func verifyAccountPassword(account accountRecord, password string) bool {
	salt, _ := hex.DecodeString(account.Salt)
	expected, _ := hex.DecodeString(account.PasswordHash)
	return hmac.Equal(expected, accountPasswordKey(password, salt, account.Iterations))
}
