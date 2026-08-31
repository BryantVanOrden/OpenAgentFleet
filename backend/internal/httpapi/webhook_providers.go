package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Provider-specific webhook handling.
//
// There was one generic endpoint: it verified an HMAC over the raw body against
// X-Hub-Signature-256, rendered a goal template, and dispatched. That is
// correct for a webhook someone wrote by hand against this API, and wrong for
// every real sender:
//
//   - GitHub signs the same way but puts the event name in a header, so every
//     push, every comment and every CI failure arrived as an indistinguishable
//     blob of JSON that the goal template had to guess at.
//   - Stripe does not use that header or that signing scheme at all. It sends
//     `Stripe-Signature: t=<unix>,v1=<hmac>` over `<t>.<body>`, so a Stripe
//     webhook pointed at this endpoint was rejected 100% of the time — the
//     feature was documented and could not work.
//   - A CRM sends a bare JSON document with no signature header any of this
//     recognised.
//
// Each sender gets its own verifier and its own summariser, so an agent is told
// "a pull request was opened against acme/api by octocat" rather than four
// kilobytes of nested JSON it has to parse before it can start.

// WebhookKind selects the verifier and the summariser.
type WebhookKind string

const (
	// KindGeneric is the original behaviour: HMAC-SHA256 over the raw body,
	// in X-Hub-Signature-256 or X-AgentFleet-Signature.
	KindGeneric WebhookKind = "generic"
	KindGitHub  WebhookKind = "github"
	KindStripe  WebhookKind = "stripe"
	// KindCRM is a bare JSON document from a CRM or form backend. It still
	// requires a signature, in whichever of the common headers the sender uses.
	KindCRM WebhookKind = "crm"
)

// stripeTolerance is how old a Stripe timestamp may be.
//
// Stripe's own guidance is five minutes. Without a bound the signature stays
// valid forever, so a captured request could be replayed indefinitely — and a
// webhook here starts an autonomous agent, which makes replay more than a
// bookkeeping problem.
const stripeTolerance = 5 * time.Minute

// ErrSignature is returned for any verification failure. Deliberately one error
// for every cause: telling a caller *why* its signature was rejected is telling
// an attacker how close they are.
var ErrSignature = errors.New("signature missing or invalid")

// normaliseKind maps a stored value onto a known kind, defaulting to generic so
// every webhook created before this existed keeps working unchanged.
func normaliseKind(k string) WebhookKind {
	switch strings.ToLower(strings.TrimSpace(k)) {
	case "github", "gh":
		return KindGitHub
	case "stripe":
		return KindStripe
	case "crm", "hubspot", "salesforce", "form":
		return KindCRM
	default:
		return KindGeneric
	}
}

// verifyFor checks the signature using the scheme the named sender uses.
func verifyFor(kind WebhookKind, secret string, body []byte, r *http.Request) error {
	switch kind {
	case KindStripe:
		return verifyStripe(secret, body, r.Header.Get("Stripe-Signature"), time.Now())
	case KindGitHub:
		// GitHub's scheme is the same HMAC the generic path uses, but only the
		// GitHub header counts: accepting X-AgentFleet-Signature on a webhook
		// declared as GitHub would let anyone who learns the URL sign with the
		// header of their choosing.
		if !hmacHexEqual(secret, body, strings.TrimPrefix(
			strings.TrimSpace(r.Header.Get("X-Hub-Signature-256")), "sha256=")) {
			return ErrSignature
		}
		return nil
	default:
		if !verifyWebhookSignature(secret, body, r) {
			return ErrSignature
		}
		return nil
	}
}

// hmacHexEqual compares a hex-encoded HMAC-SHA256 in constant time.
func hmacHexEqual(secret string, body []byte, provided string) bool {
	provided = strings.TrimSpace(provided)
	if provided == "" {
		return false
	}
	got, err := hex.DecodeString(provided)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}

// verifyStripe implements Stripe's signature scheme.
//
// The header is a comma-separated list of key=value pairs: `t` is a Unix
// timestamp and there may be several `v1` signatures during a secret rotation.
// The signed payload is the timestamp, a literal dot, and the raw body — not
// the body alone, which is why a Stripe webhook against the generic verifier
// failed every single time.
//
// now is a parameter so the tolerance window is testable without waiting.
func verifyStripe(secret string, body []byte, header string, now time.Time) error {
	if strings.TrimSpace(header) == "" {
		return ErrSignature
	}

	var timestamp string
	var signatures []string
	for _, part := range strings.Split(header, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "t":
			timestamp = strings.TrimSpace(value)
		case "v1":
			// Several during a rotation: both the old and the new secret's
			// signatures are sent, and either may be the one that matches.
			signatures = append(signatures, strings.TrimSpace(value))
		}
	}
	if timestamp == "" || len(signatures) == 0 {
		return ErrSignature
	}

	secs, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return ErrSignature
	}
	// Absolute difference, so a clock skewed in either direction is caught. A
	// timestamp far in the future is as suspicious as one far in the past.
	age := now.Sub(time.Unix(secs, 0))
	if age < 0 {
		age = -age
	}
	if age > stripeTolerance {
		return fmt.Errorf("%w: the timestamp is outside the %s tolerance",
			ErrSignature, stripeTolerance)
	}

	signed := append([]byte(timestamp+"."), body...)
	for _, sig := range signatures {
		if hmacHexEqual(secret, signed, sig) {
			return nil
		}
	}
	return ErrSignature
}

// ------------------------------------------------------------ summarisation ---

// eventSummary is what the agent is told about a delivery.
type eventSummary struct {
	// Event is the sender's own name for what happened, e.g.
	// "pull_request.opened" or "invoice.payment_failed".
	Event string
	// Headline is one sentence a model can act on without parsing the payload.
	Headline string
	// Fields are the interesting values, exposed to the goal template as
	// {{key}} in addition to the payload's own top-level keys.
	Fields map[string]string
}

// summarise extracts what happened from a delivery.
//
// The payload is still passed through to the goal in full, and is still labelled
// untrusted. This adds a headline, because an agent handed four kilobytes of
// nested GitHub JSON spends its first two turns working out what it is looking
// at, and a small model often never does.
func summarise(kind WebhookKind, body []byte, r *http.Request) eventSummary {
	switch kind {
	case KindGitHub:
		return summariseGitHub(body, r.Header.Get("X-GitHub-Event"))
	case KindStripe:
		return summariseStripe(body)
	case KindCRM:
		return summariseCRM(body)
	default:
		return eventSummary{Fields: map[string]string{}}
	}
}

// gitHubPayload is the union of the fields worth reading across event types.
// Every one is optional: a single struct beats a switch over a dozen shapes.
type gitHubPayload struct {
	Action     string `json:"action"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	Sender struct {
		Login string `json:"login"`
	} `json:"sender"`
	PullRequest *struct {
		Number  int    `json:"number"`
		Title   string `json:"title"`
		HTMLURL string `json:"html_url"`
		Merged  bool   `json:"merged"`
		Head    struct {
			Ref string `json:"ref"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
	} `json:"pull_request"`
	Issue *struct {
		Number  int    `json:"number"`
		Title   string `json:"title"`
		HTMLURL string `json:"html_url"`
	} `json:"issue"`
	Comment *struct {
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
	} `json:"comment"`
	Ref     string `json:"ref"`
	Commits []struct {
		Message string `json:"message"`
	} `json:"commits"`
	HeadCommit *struct {
		Message string `json:"message"`
	} `json:"head_commit"`
	WorkflowRun *struct {
		Name       string `json:"name"`
		Conclusion string `json:"conclusion"`
		HTMLURL    string `json:"html_url"`
	} `json:"workflow_run"`
	Release *struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	} `json:"release"`
}

func summariseGitHub(body []byte, event string) eventSummary {
	event = strings.TrimSpace(event)
	out := eventSummary{Event: event, Fields: map[string]string{}}

	var p gitHubPayload
	if err := json.Unmarshal(body, &p); err != nil {
		// A delivery that will not parse is still a delivery. Reporting the
		// event name alone beats discarding it.
		out.Headline = "GitHub sent a " + orUnknown(event) + " event"
		return out
	}

	repo := p.Repository.FullName
	who := p.Sender.Login
	set := func(k, v string) {
		if strings.TrimSpace(v) != "" {
			out.Fields[k] = v
		}
	}
	set("repo", repo)
	set("sender", who)
	set("action", p.Action)
	set("event", event)

	if p.Action != "" {
		out.Event = event + "." + p.Action
	}

	switch {
	case p.PullRequest != nil:
		set("pr_number", strconv.Itoa(p.PullRequest.Number))
		set("pr_title", p.PullRequest.Title)
		set("url", p.PullRequest.HTMLURL)
		set("branch", p.PullRequest.Head.Ref)
		set("base", p.PullRequest.Base.Ref)
		verb := p.Action
		if p.Action == "closed" && p.PullRequest.Merged {
			verb = "merged"
			out.Event = event + ".merged"
		}
		out.Headline = fmt.Sprintf("Pull request #%d %q was %s in %s by %s (%s)",
			p.PullRequest.Number, p.PullRequest.Title, orUnknown(verb),
			orUnknown(repo), orUnknown(who), p.PullRequest.HTMLURL)

	case p.WorkflowRun != nil:
		set("workflow", p.WorkflowRun.Name)
		set("conclusion", p.WorkflowRun.Conclusion)
		set("url", p.WorkflowRun.HTMLURL)
		out.Headline = fmt.Sprintf("The %q workflow on %s concluded: %s (%s)",
			p.WorkflowRun.Name, orUnknown(repo),
			orUnknown(p.WorkflowRun.Conclusion), p.WorkflowRun.HTMLURL)

	case p.Comment != nil && p.Issue != nil:
		set("issue_number", strconv.Itoa(p.Issue.Number))
		set("issue_title", p.Issue.Title)
		set("comment", p.Comment.Body)
		set("url", p.Comment.HTMLURL)
		out.Headline = fmt.Sprintf("%s commented on #%d %q in %s (%s)",
			orUnknown(who), p.Issue.Number, p.Issue.Title,
			orUnknown(repo), p.Comment.HTMLURL)

	case p.Issue != nil:
		set("issue_number", strconv.Itoa(p.Issue.Number))
		set("issue_title", p.Issue.Title)
		set("url", p.Issue.HTMLURL)
		out.Headline = fmt.Sprintf("Issue #%d %q was %s in %s by %s (%s)",
			p.Issue.Number, p.Issue.Title, orUnknown(p.Action),
			orUnknown(repo), orUnknown(who), p.Issue.HTMLURL)

	case p.Release != nil:
		set("tag", p.Release.TagName)
		set("url", p.Release.HTMLURL)
		out.Headline = fmt.Sprintf("Release %s of %s was %s (%s)",
			p.Release.TagName, orUnknown(repo), orUnknown(p.Action), p.Release.HTMLURL)

	case p.Ref != "":
		branch := strings.TrimPrefix(p.Ref, "refs/heads/")
		set("branch", branch)
		set("ref", p.Ref)
		msg := ""
		if p.HeadCommit != nil {
			msg = firstLine(p.HeadCommit.Message)
		} else if len(p.Commits) > 0 {
			msg = firstLine(p.Commits[0].Message)
		}
		set("message", msg)
		out.Headline = fmt.Sprintf("%d commit(s) pushed to %s of %s by %s: %s",
			max(len(p.Commits), 1), branch, orUnknown(repo), orUnknown(who), msg)

	default:
		out.Headline = fmt.Sprintf("GitHub sent a %s event for %s",
			orUnknown(event), orUnknown(repo))
	}
	return out
}

// stripePayload is the envelope every Stripe event shares.
type stripePayload struct {
	Type string `json:"type"`
	Data struct {
		Object map[string]any `json:"object"`
	} `json:"data"`
}

func summariseStripe(body []byte) eventSummary {
	out := eventSummary{Fields: map[string]string{}}

	var p stripePayload
	if err := json.Unmarshal(body, &p); err != nil {
		out.Headline = "Stripe sent an event that could not be parsed"
		return out
	}
	out.Event = p.Type
	out.Fields["event"] = p.Type

	obj := p.Data.Object
	get := func(key string) string {
		if v, ok := obj[key]; ok {
			return scalarString(v)
		}
		return ""
	}

	// Amounts are in the currency's minor unit, so 1999 means $19.99. Rendering
	// the raw integer into a goal would tell an agent a customer was charged
	// nineteen hundred dollars.
	amount := ""
	for _, key := range []string{"amount", "amount_due", "amount_paid", "amount_total"} {
		if raw, ok := obj[key]; ok {
			if cents, err := strconv.ParseFloat(scalarString(raw), 64); err == nil {
				amount = fmt.Sprintf("%.2f", cents/100)
				break
			}
		}
	}
	currency := strings.ToUpper(get("currency"))

	for key, name := range map[string]string{
		"id": "object_id", "customer": "customer", "status": "status",
		"customer_email": "customer_email", "number": "invoice_number",
		"hosted_invoice_url": "url", "receipt_url": "receipt_url",
	} {
		if v := get(key); v != "" {
			out.Fields[name] = v
		}
	}
	if amount != "" {
		out.Fields["amount"] = amount
		out.Fields["currency"] = currency
	}

	money := ""
	if amount != "" {
		money = " for " + amount + " " + currency
	}
	who := firstNonEmptyStr(get("customer_email"), get("customer"))
	if who != "" {
		who = " (" + who + ")"
	}
	out.Headline = fmt.Sprintf("Stripe: %s%s%s", orUnknown(p.Type), money, who)
	return out
}

// summariseCRM handles a bare JSON document from a CRM or form backend.
//
// There is no standard here, so this looks for the fields these payloads
// overwhelmingly use rather than pretending to understand one vendor's schema.
func summariseCRM(body []byte) eventSummary {
	out := eventSummary{Fields: map[string]string{}}

	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		out.Headline = "A CRM sent a payload that could not be parsed"
		return out
	}

	// Nested one level: HubSpot and Salesforce both wrap the record in an
	// envelope, and the useful fields are inside it.
	flat := map[string]any{}
	for k, v := range doc {
		flat[strings.ToLower(k)] = v
		if nested, ok := v.(map[string]any); ok {
			for nk, nv := range nested {
				key := strings.ToLower(nk)
				if _, taken := flat[key]; !taken {
					flat[key] = nv
				}
			}
		}
	}

	pick := func(names ...string) string {
		for _, n := range names {
			if v, ok := flat[n]; ok {
				if s := strings.TrimSpace(scalarString(v)); s != "" {
					return s
				}
			}
		}
		return ""
	}

	email := pick("email", "email_address", "contact_email", "from")
	name := pick("name", "full_name", "fullname", "firstname", "contact_name", "company")
	subject := pick("subject", "title", "topic", "form_name")
	message := pick("message", "body", "notes", "description", "comments")
	event := pick("event", "type", "eventtype", "subscriptiontype", "action")
	stage := pick("stage", "dealstage", "status", "pipeline")

	for k, v := range map[string]string{
		"email": email, "name": name, "subject": subject,
		"message": message, "event": event, "stage": stage,
	} {
		if v != "" {
			out.Fields[k] = v
		}
	}
	out.Event = firstNonEmptyStr(event, "crm.record")

	switch {
	case email != "" || name != "":
		out.Headline = fmt.Sprintf("CRM %s: %s%s%s",
			out.Event,
			firstNonEmptyStr(name, email),
			ifSet(" <", email, ">", name != "" && email != ""),
			ifSet(" — ", firstNonEmptyStr(subject, message), "", subject != "" || message != ""))
	case subject != "" || message != "":
		out.Headline = fmt.Sprintf("CRM %s: %s", out.Event,
			firstNonEmptyStr(subject, message))
	default:
		out.Headline = "A CRM sent a " + out.Event + " payload"
	}
	return out
}

// ---------------------------------------------------------------- rendering ---

// renderProviderGoal builds the goal from the template, the summary and the body.
//
// The summary's fields are substituted first so {{repo}} and {{pr_number}} work
// on a GitHub webhook, then the payload's own top-level keys, then the payload
// itself. The untrusted-data warning is kept and applies to the headline too:
// a pull request title is attacker-controlled text.
func renderProviderGoal(tmpl string, sum eventSummary, body []byte) string {
	// Answered against the template the operator wrote, before anything is
	// substituted into it. Substituting first and asking afterwards is how a
	// goal that used {{repo}} also got four kilobytes of payload appended
	// underneath it: every placeholder was already gone by then.
	bare := !strings.Contains(tmpl, "{{")

	out := tmpl
	for k, v := range sum.Fields {
		out = strings.ReplaceAll(out, "{{"+k+"}}", v)
	}
	if sum.Headline != "" {
		out = strings.ReplaceAll(out, "{{summary}}", sum.Headline)
	}

	// Payload keys and {{payload}}, without the trailing block.
	out = substituteGoal(out, body)

	// Prepended, not appended: what happened belongs at the top, where a small
	// model reading a long payload will still see it. Only when the template
	// did not already place it.
	if sum.Headline != "" && !strings.Contains(tmpl, "{{summary}}") {
		out = "What happened: " + sum.Headline + "\n\n" + out
	}
	return appendPayloadIfBare(out, body, bare)
}

// ------------------------------------------------------------------ helpers ---

func orUnknown(s string) string {
	if strings.TrimSpace(s) == "" {
		return "unknown"
	}
	return s
}

func firstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

func ifSet(prefix, value, suffix string, when bool) string {
	if !when || strings.TrimSpace(value) == "" {
		return ""
	}
	return prefix + value + suffix
}
