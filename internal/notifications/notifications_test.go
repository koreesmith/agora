package notifications

import "testing"

// AGORA-142: SendHTML used to splice `to` directly into a raw "To: "+to SMTP
// header with no validation beyond whatever the caller happened to do
// upstream, letting an address containing CR/LF inject extra headers (Bcc,
// additional recipients, ...). It must now reject anything that isn't a
// single well-formed address before it ever reaches header construction —
// checked here via a zero-value EmailService, since a rejected address
// should return before touching smtpConfig()/db at all.
func TestSendHTMLRejectsMalformedRecipient(t *testing.T) {
	cases := []struct {
		name    string
		to      string
		wantErr bool
	}{
		{"CRLF header injection attempt", "victim@x.com\r\nBcc: mass@list.com", true},
		{"bare LF", "victim@x.com\nBcc: mass@list.com", true},
		{"empty", "", true},
		{"not an address", "not-an-email", true},
	}

	e := &EmailService{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := e.SendHTML(tc.to, "subject", "body", "", "")
			if (err != nil) != tc.wantErr {
				t.Errorf("SendHTML(to=%q) error = %v, wantErr %v", tc.to, err, tc.wantErr)
			}
		})
	}
}
