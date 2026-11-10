package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"slices"
	"strings"

	"github.com/zopping/gateway-svc/policy"
)

// checkBody applies a shopper's body rules: only the allowed fields, only the allowed values, and an own-id field that is theirs.
// Keys are compared in any letter case, because Go's JSON decoder fills a struct field from a key of any case: {"STATUS": "PACKED"}
// reaches a status field and must be caught here. Only JSON is accepted, since GoFr also binds form bodies this cannot read.
func checkBody(r *http.Request, e Entry, customerID, maxBody int64) error {
	if r.Body == nil || r.Body == http.NoBody {
		return nil
	}

	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return &policy.UnsupportedMedia{Reason: "this endpoint accepts application/json only"}
	}

	raw, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil {
		return err
	}

	if int64(len(raw)) > maxBody {
		return policy.TooLarge
	}

	r.Body = io.NopCloser(bytes.NewReader(raw))
	r.ContentLength = int64(len(raw))

	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil // not an object: the backend rejects it with a 400 as well
	}

	for key, value := range fields {
		lower := strings.ToLower(key)

		if e.OnlyBody != nil && !slices.ContainsFunc(e.OnlyBody, func(a string) bool { return strings.EqualFold(a, key) }) {
			return &policy.Denied{Reason: fmt.Sprintf("you may not set %s", key)}
		}

		if allowed, ok := e.BodyValues[lower]; ok {
			var s string
			if json.Unmarshal(value, &s) != nil || !slices.Contains(allowed, s) {
				return &policy.Denied{Reason: fmt.Sprintf("you may only set %s to %s", key, strings.Join(allowed, ", "))}
			}
		}

		if e.OwnBody != "" && strings.EqualFold(key, e.OwnBody) {
			var n json.Number
			if json.Unmarshal(value, &n) != nil || n.String() != fmt.Sprint(customerID) {
				return &policy.Denied{Reason: fmt.Sprintf("%s must be your own", e.OwnBody)}
			}
		}
	}

	return nil
}
