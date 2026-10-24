package catalog

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"net/mail"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// FieldType is the kind of an install form field.
type FieldType string

const (
	FieldText     FieldType = "text"
	FieldPassword FieldType = "password"
	FieldEmail    FieldType = "email"
	FieldNumber   FieldType = "number"
	FieldBoolean  FieldType = "boolean"
	FieldURL      FieldType = "url"
	FieldIP       FieldType = "ip"
	FieldFQDN     FieldType = "fqdn"
	FieldFQDNIP   FieldType = "fqdnip"
	FieldRandom   FieldType = "random" // generated secret, never asked for
)

// FormField is one install-time setting, delivered to the compose template
// as the environment variable EnvVariable.
type FormField struct {
	Type         FieldType `json:"type"`
	Label        string    `json:"label"`
	Hint         string    `json:"hint,omitempty"`
	Placeholder  string    `json:"placeholder,omitempty"`
	EnvVariable  string    `json:"env_variable"`
	Required     bool      `json:"required"`
	Min          *int      `json:"min,omitempty"` // length for strings, value for numbers
	Max          *int      `json:"max,omitempty"`
	Default      any       `json:"default,omitempty"`
	Options      []Option  `json:"options,omitempty"`
	Regex        string    `json:"regex,omitempty"`
	PatternError string    `json:"pattern_error,omitempty"`
	Encoding     string    `json:"encoding,omitempty"` // "base64" for random fields
}

type Option struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// UnmarshalJSON tolerates option values written as numbers or booleans.
func (o *Option) UnmarshalJSON(data []byte) error {
	var raw struct {
		Label string `json:"label"`
		Value any    `json:"value"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	o.Label, o.Value = raw.Label, stringify(raw.Value)
	return nil
}

func stringify(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	default:
		data, _ := json.Marshal(v)
		return string(data)
	}
}

// DefaultValue returns the field's default as a string.
func (f FormField) DefaultValue() string { return stringify(f.Default) }

// FieldErrors maps environment variable names to validation messages.
type FieldErrors map[string]string

func (e FieldErrors) Error() string {
	parts := make([]string, 0, len(e))
	for key, msg := range e {
		parts = append(parts, key+": "+msg)
	}
	slices.Sort(parts)
	return "invalid settings: " + strings.Join(parts, "; ")
}

// ResolveValues validates user input against the app's form and returns the
// final environment values. Random fields keep their previous value (so
// secrets survive updates) or are generated. Empty optional fields fall back
// to previous values, then defaults.
func (a *App) ResolveValues(input, previous map[string]string) (map[string]string, error) {
	values := map[string]string{}
	errs := FieldErrors{}
	for _, f := range a.FormFields {
		key := f.EnvVariable
		if key == "" {
			continue
		}
		if f.Type == FieldRandom {
			switch {
			case previous[key] != "":
				values[key] = previous[key]
			case strings.TrimSpace(input[key]) != "":
				values[key] = strings.TrimSpace(input[key])
			default:
				secret, err := f.generate()
				if err != nil {
					return nil, err
				}
				values[key] = secret
			}
			continue
		}
		value, given := input[key]
		value = strings.TrimSpace(value)
		if !given {
			value = previous[key]
		}
		if value == "" && !given {
			value = f.DefaultValue()
		}
		if value == "" {
			if f.Required {
				errs[key] = "required"
			}
			values[key] = ""
			continue
		}
		if msg := f.validate(value); msg != "" {
			errs[key] = msg
			continue
		}
		if f.Type == FieldBoolean {
			value = strconv.FormatBool(value == "true" || value == "1")
		}
		values[key] = value
	}
	if len(errs) > 0 {
		return nil, errs
	}
	return values, nil
}

var fqdnPattern = regexp.MustCompile(`^(?i)([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}\.?$`)

func (f FormField) validate(value string) string {
	if len(f.Options) > 0 {
		if !slices.ContainsFunc(f.Options, func(o Option) bool { return o.Value == value }) {
			return "must be one of the listed options"
		}
		return ""
	}
	switch f.Type {
	case FieldNumber:
		n, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return "must be a number"
		}
		if f.Min != nil && n < float64(*f.Min) {
			return fmt.Sprintf("must be at least %d", *f.Min)
		}
		if f.Max != nil && n > float64(*f.Max) {
			return fmt.Sprintf("must be at most %d", *f.Max)
		}
		return ""
	case FieldBoolean:
		switch value {
		case "true", "false", "1", "0":
			return ""
		}
		return "must be true or false"
	case FieldEmail:
		if addr, err := mail.ParseAddress(value); err != nil || addr.Address != value {
			return "must be an email address"
		}
	case FieldURL:
		if u, err := url.Parse(value); err != nil || u.Scheme == "" || u.Host == "" {
			return "must be an absolute URL"
		}
	case FieldIP:
		if net.ParseIP(value) == nil {
			return "must be an IP address"
		}
	case FieldFQDN:
		if !fqdnPattern.MatchString(value) {
			return "must be a domain name"
		}
	case FieldFQDNIP:
		if net.ParseIP(value) == nil && !fqdnPattern.MatchString(value) {
			return "must be a domain name or IP address"
		}
	}
	length := len([]rune(value))
	if f.Min != nil && length < *f.Min {
		return fmt.Sprintf("must be at least %d characters", *f.Min)
	}
	if f.Max != nil && length > *f.Max {
		return fmt.Sprintf("must be at most %d characters", *f.Max)
	}
	if f.Regex != "" {
		re, err := regexp.Compile(f.Regex)
		if err == nil && !re.MatchString(value) {
			if f.PatternError != "" {
				return f.PatternError
			}
			return "has an invalid format"
		}
	}
	return ""
}

const secretAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// generate creates a random secret: Min characters (default 32) from an
// alphanumeric alphabet, or Min random bytes base64-encoded.
func (f FormField) generate() (string, error) {
	n := 32
	if f.Min != nil && *f.Min > 0 {
		n = *f.Min
	}
	if f.Encoding == "base64" {
		buf := make([]byte, n)
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		return base64.StdEncoding.EncodeToString(buf), nil
	}
	out := make([]byte, n)
	limit := big.NewInt(int64(len(secretAlphabet)))
	for i := range out {
		idx, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", err
		}
		out[i] = secretAlphabet[idx.Int64()]
	}
	return string(out), nil
}
