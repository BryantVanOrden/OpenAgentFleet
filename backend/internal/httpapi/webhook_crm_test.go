package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// HubSpot and Salesforce used to be aliases for a field-name heuristic. These
// pin the real schemes: HubSpot's v3 and v1 signatures computed the way HubSpot
// computes them, and Salesforce's XML outbound message with its org-id check.

const hubspotBody = `[{"subscriptionType":"deal.propertyChange","objectId":12345,` +
	`"propertyName":"dealstage","propertyValue":"closedwon","changeSource":"CRM_UI","portalId":99}]`

func TestHubSpotV3SignatureIsVerifiedTheWayHubSpotComputesIt(t *testing.T) {
	secret := "hs-secret"
	now := time.Now()
	tsHeader := strconv.FormatInt(now.UnixMilli(), 10)

	r := httptest.NewRequest("POST", "https://fleet.example.com/api/webhooks/tok-abc", strings.NewReader(hubspotBody))
	r.Host = "fleet.example.com"
	r.Header.Set("X-HubSpot-Request-Timestamp", tsHeader)

	// The signature base is method + full URI + body + timestamp — the URI
	// HubSpot POSTed to, not just the path.
	base := "POST" + "https://fleet.example.com/api/webhooks/tok-abc" + hubspotBody + tsHeader
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(base))
	r.Header.Set("X-HubSpot-Signature-v3", base64.StdEncoding.EncodeToString(mac.Sum(nil)))

	if err := verifyHubSpot(secret, []byte(hubspotBody), r, now); err != nil {
		t.Fatalf("a correctly signed v3 delivery was rejected: %v", err)
	}

	// The same delivery an hour later is a replay, not a delivery.
	if err := verifyHubSpot(secret, []byte(hubspotBody), r, now.Add(time.Hour)); err == nil {
		t.Fatal("a stale v3 timestamp was accepted")
	}

	// A signature over a tampered body must fail.
	if err := verifyHubSpot(secret, []byte(`[{"objectId":1}]`), r, now); err == nil {
		t.Fatal("a tampered body passed v3 verification")
	}
}

func TestHubSpotV1SignatureStillWorks(t *testing.T) {
	secret := "hs-secret"
	r := httptest.NewRequest("POST", "/api/webhooks/tok-abc", strings.NewReader(hubspotBody))
	sum := sha256.Sum256(append([]byte(secret), []byte(hubspotBody)...))
	r.Header.Set("X-HubSpot-Signature", hex.EncodeToString(sum[:]))

	if err := verifyHubSpot(secret, []byte(hubspotBody), r, time.Now()); err != nil {
		t.Fatalf("a correct v1 signature was rejected: %v", err)
	}
	if err := verifyHubSpot("wrong-secret", []byte(hubspotBody), r, time.Now()); err == nil {
		t.Fatal("a v1 signature with the wrong secret was accepted")
	}
}

func TestHubSpotSummaryReadsTheRealSchema(t *testing.T) {
	sum := summariseHubSpot([]byte(hubspotBody))
	if !strings.Contains(sum.Headline, "deal.propertyChange") ||
		!strings.Contains(sum.Headline, "dealstage") {
		t.Errorf("headline %q does not name the subscription and property", sum.Headline)
	}
	if sum.Fields["object_id"] != "12345" || sum.Fields["new_value"] != "closedwon" {
		t.Errorf("fields = %+v", sum.Fields)
	}
	// Not the heuristic: a payload that is not HubSpot's array says so.
	bad := summariseHubSpot([]byte(`{"email":"x@y.z"}`))
	if !strings.Contains(bad.Headline, "not the expected event array") {
		t.Errorf("a non-HubSpot payload was guessed at: %q", bad.Headline)
	}
}

const salesforceBody = `<?xml version="1.0" encoding="UTF-8"?>
<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/"
  xmlns="http://soap.sforce.com/2005/09/outbound" xmlns:sf="urn:sobject.enterprise.soap.sforce.com">
  <soapenv:Body>
    <notifications>
      <OrganizationId>00D000000000062EAA</OrganizationId>
      <ActionId>04k000000000001AAA</ActionId>
      <Notification>
        <Id>04l000000000001AAA</Id>
        <sObject xsi:type="sf:Opportunity" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
          <sf:Id>006000000000001AAA</sf:Id>
          <sf:Name>Big Renewal</sf:Name>
          <sf:StageName>Negotiation</sf:StageName>
          <sf:Amount>50000.0</sf:Amount>
        </sObject>
      </Notification>
    </notifications>
  </soapenv:Body>
</soapenv:Envelope>`

func TestSalesforceOrgIDIsTheCredential(t *testing.T) {
	// The 15- and 18-character forms of the same org id both match.
	if err := verifySalesforce("00D000000000062EAA", []byte(salesforceBody)); err != nil {
		t.Fatalf("the 18-char org id was rejected: %v", err)
	}
	if err := verifySalesforce("00D000000000062", []byte(salesforceBody)); err != nil {
		t.Fatalf("the 15-char org id was rejected: %v", err)
	}
	if err := verifySalesforce("00D999999999999XXX", []byte(salesforceBody)); err == nil {
		t.Fatal("a delivery from the wrong org was accepted")
	}
	if err := verifySalesforce("", []byte(salesforceBody)); err == nil {
		t.Fatal("an empty expected org id must refuse everything, not accept everything")
	}
	if err := verifySalesforce("00D000000000062EAA", []byte(`{"not":"xml"}`)); err == nil {
		t.Fatal("a JSON body passed as a Salesforce outbound message")
	}
}

func TestSalesforceSummaryReadsTheSObject(t *testing.T) {
	sum := summariseSalesforce([]byte(salesforceBody))
	if !strings.Contains(sum.Headline, "Opportunity") || !strings.Contains(sum.Headline, "Big Renewal") {
		t.Errorf("headline %q does not name the object", sum.Headline)
	}
	if sum.Fields["stagename"] != "Negotiation" || sum.Fields["record_id"] != "006000000000001AAA" {
		t.Errorf("fields = %+v", sum.Fields)
	}
}

func TestSalesforceAckIsSOAP(t *testing.T) {
	// Salesforce marks the delivery failed and retries for 24 hours unless the
	// response is a SOAP Ack — a JSON 202 would run the same mission repeatedly.
	w := httptest.NewRecorder()
	writeSalesforceAck(w)
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/xml") {
		t.Errorf("content type = %q, want text/xml", ct)
	}
	if !strings.Contains(w.Body.String(), "<Ack>true</Ack>") {
		t.Errorf("body is not an Ack: %s", w.Body.String())
	}
}
