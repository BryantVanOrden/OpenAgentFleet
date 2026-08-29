-- Migration 0018: model combinations.
--
-- Until now a bot had one ordered list of providers and every kind of thinking
-- went to whichever answered first — the same model reading pixels, planning,
-- and summarising. A combination assigns models to roles instead: a fast
-- multimodal model for the hands, a deep reasoner for the brain.
--
-- Combinations sit in the same fallback chain as plain providers, so a bot can
-- prefer a combination and fall back to a single model, or the reverse.
CREATE TABLE IF NOT EXISTS model_combos (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS model_combo_roles (
    combo_id    TEXT NOT NULL REFERENCES model_combos(id) ON DELETE CASCADE,
    -- 'vision', 'reasoning', 'chat', 'summarize', 'refine'.
    role        TEXT NOT NULL,
    provider_id TEXT NOT NULL,
    PRIMARY KEY (combo_id, role)
);

CREATE INDEX IF NOT EXISTS combo_roles_provider_idx ON model_combo_roles(provider_id);
