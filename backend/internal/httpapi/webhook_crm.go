package httpapi

import (
	"crypto/hmac"
	"encoding/json"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Vendor-real CRM webhooks: HubSpot and Salesforce.
//
// "CRM" used to be one heuristic — a field-name search over whatever JSON
// arrived, covering what HubSpot, Salesforce and form backends happen to send.
// That stays, as the explicitly-generic `crm` kind for form backends. But the
// two vendors people actually mean by "CRM webhook" now have their own kinds,
// with their real schemas and their real authentication:
//
//   - HubSpot signs with HMAC (v3 preferred, v1 accepted) and sends a JSON
//     array of subscription events.
//   - Salesforce outbound messages are SOAP XML, carry no signature header at
//     all — Salesforce's own guidance is URL secrecy — and expect a SOAP Ack
//     back, without which Salesforce retries for 24 hours and then flags the
//     delivery failed.

// hubspotTolerance bounds the v3 timestamp, per HubSpot's own guidance.
const hubspotTolerance = 5 * time.Minute

// verifyHubSpot accepts either of HubSpot's current signature schemes.
//
// v3: base64(HMAC-SHA256(secret, method + uri + body + timestamp)), in
// X-HubSpot-Signature-v3, with the timestamp in X-HubSpot-Request-Timestamp.
// The uri is the exact URL HubSpot POSTed to, which behind a proxy has to be
// reconstructed from forwarding headers — the one operational caveat.
//
// v1: hex(SHA-256(secret + body)), in X-HubSpot-Signature. Older, no
// timestamp, kept because most existing HubSpot app configs still send it.
func verifyHubSpot(secret string, body []byte, r *http.Request, now time.Time) error {
	if v3 := strings.TrimSpace(r.Header.Get("X-HubSpot-Signature-v3")); v3 != "" {
		tsHeader := strings.TrimSpace(r.Header.Get("X-HubSpot-Request-Timestamp"))
		tsMillis, err := strconv.ParseInt(tsHeader, 10, 64)
		if err != nil {
			return ErrSignature
		}
		ts := time.UnixMilli(tsMillis)
		if now.Sub(ts) > hubspotTolerance || ts.Sub(now) > hubspotTolerance {
			return ErrSignature // replay window, same rule as Stripe
		}

		base := r.Method + requestURI(r) + string(body) + tsHeader
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(base))
		want := base64.StdEncoding.EncodeToString(mac.Sum(nil))
		if subtle.ConstantTimeCompare([]byte(want), []byte(v3)) == 1 {
			return nil
		}
		return ErrSignature
	}

	if v1 := strings.TrimSpace(r.Header.Get("X-HubSpot-Signature")); v1 != "" {
		sum := sha256.Sum256(append([]byte(secret), body...))
		want := hex.EncodeToString(sum[:])
		if subtle.ConstantTimeCompare([]byte(want), []byte(strings.ToLower(v1))) == 1 {
			return nil
		}
	}
	return ErrSignature
}

// requestURI reconstructs the URL HubSpot signed.
//
// Direct exposure: scheme comes from the connection. Behind a reverse proxy the
// standard forwarding headers win, because HubSpot signed the public URL, not
// the internal one the proxy forwarded to.
func requestURI(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if fp := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); fp != "" {
		scheme = fp
	}
	host := r.Host
	if fh := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); fh != "" {
		host = fh
	}
	return scheme + "://" + host + r.URL.RequestURI()
}

// hubspotEvent is one entry in the JSON array HubSpot POSTs.
type hubspotEvent struct {
	SubscriptionType string `json:"subscriptionType"`
	ObjectID         int64  `json:"objectId"`
	PropertyName     string `json:"propertyName"`
	PropertyValue    string `json:"propertyValue"`
	ChangeSource     string `json:"changeSource"`
	PortalID         int64  `json:"portalId"`
	OccurredAt       int64  `json:"occurredAt"`
}

// summariseHubSpot reads HubSpot's real schema instead of guessing field names.
func summariseHubSpot(body []byte) eventSummary {
	out := eventSummary{Fields: map[string]string{}}

	var events []hubspotEvent
	if err := json.Unmarshal(body, &events); err != nil || len(events) == 0 {
		out.Headline = "HubSpot sent a payload that is not the expected event array"
		return out
	}

	first := events[0]
	out.Fields["subscription_type"] = first.SubscriptionType
	out.Fields["object_id"] = strconv.FormatInt(first.ObjectID, 10)
	if first.PropertyName != "" {
		out.Fields["property"] = first.PropertyName
		out.Fields["new_value"] = clipText(first.PropertyValue, 200)
	}
	if first.ChangeSource != "" {
		out.Fields["change_source"] = first.ChangeSource
	}
	out.Fields["events"] = strconv.Itoa(len(events))

	switch {
	case first.PropertyName != "":
		out.Headline = fmt.Sprintf("HubSpot: %s — %s changed on object %d",
			first.SubscriptionType, first.PropertyName, first.ObjectID)
	default:
		out.Headline = fmt.Sprintf("HubSpot: %s on object %d", first.SubscriptionType, first.ObjectID)
	}
	if len(events) > 1 {
		out.Headline += fmt.Sprintf(" (and %d more events in this delivery)", len(events)-1)
	}
	return out
}

// ------------------------------------------------------------- salesforce ---

// salesforceEnvelope is the SOAP outbound message, reduced to what matters.
//
// xml.Unmarshal matches local names regardless of namespace prefix, which is
// what makes one struct work across the prefixes Salesforce orgs actually emit.
type salesforceEnvelope struct {
	XMLName xml.Name `xml:"Envelope"`
	Body    struct {
		Notifications struct {
			OrganizationID string `xml:"OrganizationId"`
			Notification   []struct {
				ID      string `xml:"Id"`
				SObject struct {
					Type   string     `xml:"type,attr"`
					Fields []xmlField `xml:",any"`
				} `xml:"sObject"`
			} `xml:"Notification"`
		} `xml:"notifications"`
	} `xml:"Body"`
}

type xmlField struct {
	XMLName xml.Name
	Value   string `xml:",chardata"`
}

// verifySalesforce authenticates an outbound message.
//
// Salesforce sends no signature header — its own model is TLS plus URL
// secrecy, and the unguessable token in our URL is that secret. The stored
// webhook secret does double duty here as the expected OrganizationId: the
// payload's org id must match it, so a delivery needs both the URL and the
// right org. Compared prefix-wise because Salesforce writes the same id in
// 15- and 18-character forms.
func verifySalesforce(expectedOrgID string, body []byte) error {
	var env salesforceEnvelope
	if err := xml.Unmarshal(body, &env); err != nil {
		return ErrSignature
	}
	got := strings.TrimSpace(env.Body.Notifications.OrganizationID)
	want := strings.TrimSpace(expectedOrgID)
	if got == "" || want == "" {
		return ErrSignature
	}
	short := func(s string) string {
		if len(s) > 15 {
			return s[:15]
		}
		return s
	}
	if subtle.ConstantTimeCompare([]byte(short(got)), []byte(short(want))) != 1 {
		return ErrSignature
	}
	return nil
}

// summariseSalesforce reads the outbound message's sObject.
func summariseSalesforce(body []byte) eventSummary {
	out := eventSummary{Fields: map[string]string{}}
	var env salesforceEnvelope
	if err := xml.Unmarshal(body, &env); err != nil {
		out.Headline = "Salesforce sent a payload that is not an outbound message"
		return out
	}
	notes := env.Body.Notifications.Notification
	if len(notes) == 0 {
		out.Headline = "Salesforce outbound message with no notifications"
		return out
	}

	first := notes[0]
	objType := strings.TrimPrefix(first.SObject.Type, "sf:")
	out.Fields["object_type"] = objType
	for _, f := range first.SObject.Fields {
		name := strings.ToLower(f.XMLName.Local)
		val := strings.TrimSpace(f.Value)
		if val == "" {
			continue
		}
		switch name {
		case "id":
			out.Fields["record_id"] = val
		case "name", "subject", "email", "stagename", "status", "amount":
			out.Fields[name] = clipText(val, 200)
		}
	}
	out.Fields["notifications"] = strconv.Itoa(len(notes))

	label := out.Fields["name"]
	if label == "" {
		label = out.Fields["subject"]
	}
	if label == "" {
		label = out.Fields["record_id"]
	}
	out.Headline = fmt.Sprintf("Salesforce: %s %q updated", orDefaultStr(objType, "record"), label)
	if len(notes) > 1 {
		out.Headline += fmt.Sprintf(" (and %d more records in this delivery)", len(notes)-1)
	}
	return out
}

// salesforceAck is the SOAP acknowledgement Salesforce requires.
//
// Without it, Salesforce treats the delivery as failed and retries for 24
// hours before flagging the outbound message — so a webhook that dispatched
// the task but answered JSON would run every mission several times.
const salesforceAck = `<?xml version="1.0" encoding="UTF-8"?>
<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/">
  <soapenv:Body>
    <notificationsResponse xmlns="http://soap.sforce.com/2005/09/outbound">
      <Ack>true</Ack>
    </notificationsResponse>
  </soapenv:Body>
</soapenv:Envelope>`

func writeSalesforceAck(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(salesforceAck))
}

func orDefaultStr(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}
