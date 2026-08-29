-- Per-instance sudo, alongside the existing shell_access flag.
--
-- These are enforced in different places and behave differently, which the UI
-- has to reflect:
--
--   shell_access is checked by the orchestrator on every shell action, so it
--   can be turned on and off on a running instance and takes effect at once.
--
--   sudo_access decides whether the container gets docker's no-new-privileges
--   option, which the kernel applies at creation. It cannot be changed on a
--   running container -- the instance has to be recreated -- and turning it on
--   removes the control that actually stops sudo working inside the sandbox.
--   Defaulting to false keeps every existing instance exactly as it is.
ALTER TABLE instances
    ADD COLUMN IF NOT EXISTS sudo_access BOOLEAN NOT NULL DEFAULT FALSE;
