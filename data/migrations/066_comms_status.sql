-- 066_comms_status.sql
-- Approval items in Communications.
--
-- 1. person_messages.hil_ref links an outbound row to the Human-in-Loop item
--    (hil_pending.id) that gates it. The row is created when the item is
--    queued and the SAME row follows it. person_messages.status (015) takes
--    the new values pending_approval, approved, sending, rejected and
--    expired next to the existing draft, sent and failed. Rows that exist
--    keep their status (default 'sent') and get no hil_ref.
-- 2. people.kind says what a row is: 'person' (default) or 'company'. A
--    company is a people row too, so anything we write to has one place.
--    Unrelated to people.category, which is the review state.
ALTER TABLE person_messages ADD COLUMN hil_ref TEXT;
ALTER TABLE people ADD COLUMN kind TEXT NOT NULL DEFAULT 'person';

CREATE INDEX IF NOT EXISTS idx_person_messages_hil_ref ON person_messages(hil_ref);
