-- Which sender a webhook is for, and therefore which signature scheme applies.
--
-- There was one generic scheme: HMAC-SHA256 over the raw body in
-- X-Hub-Signature-256. GitHub happens to match it; Stripe does not -- it signs
-- "<timestamp>.<body>" and sends the result in Stripe-Signature -- so a Stripe
-- webhook pointed at this endpoint was rejected on every single delivery.
--
-- Stored rather than sniffed from the request headers on purpose. Picking the
-- verifier by looking at which header arrived would let the caller choose its
-- own scheme, and the entire point of the signature is that the caller does not
-- get to choose.
--
-- Defaulted to 'generic' so every webhook that already exists keeps behaving
-- exactly as it does today.
ALTER TABLE webhooks
    ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'generic';
