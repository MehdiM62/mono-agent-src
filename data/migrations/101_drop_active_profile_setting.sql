-- 101_drop_active_profile_setting.sql
-- There is no active profile any more: every command that acts on a profile
-- is told which one with --profile (see the strict explicit profile plan).
-- Migration 011 seeded settings.active_profile_id and `profile switch` wrote
-- it; nothing reads it now, so the row goes. Idempotent: a database that
-- never had the row, or already lost it, is unchanged.
DELETE FROM settings WHERE key = 'active_profile_id';
