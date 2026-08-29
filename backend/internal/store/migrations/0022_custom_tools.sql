-- Migration 0022: operator-added tools.
--
-- The archetype catalogue cannot know about a company's internal CLI, and
-- editing a file inside the sandbox image to add one is not a workflow. A bot
-- can carry its own tools with how to fetch them.
ALTER TABLE instances ADD COLUMN IF NOT EXISTS custom_tools TEXT;
