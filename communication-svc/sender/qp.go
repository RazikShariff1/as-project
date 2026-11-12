package sender

import (
	"bytes"
	"mime/quotedprintable"
)

func quotedPrintable(s string) string {
	var buf bytes.Buffer

	w := quotedprintable.NewWriter(&buf)
	_, _ = w.Write([]byte(s))
	_ = w.Close()

	return buf.String()
}
