-- 107_person_messages_sent_at_utc.sql
-- A message's sent_at given in a zone other than UTC (`people message add
-- --sent-at` or an import with an RFC3339 offset) was stored as Go's time
-- text with a numeric zone name, "2026-10-19 12:00:00 +0330 +0330". The
-- driver cannot read that back, so listing that person's messages failed,
-- and as text it ordered by local time. sent_at is now written in UTC; this
-- rewrites the stored ones to the UTC instant ("2026-10-19 08:30:00.000").
--
-- k is the position, after the 19 characters of date and time, of the space
-- before the "±hhmm" offset; the value is rebuilt as "date time[.frac]±hh:mm",
-- which SQLite reads, and strftime gives it back in UTC. A value it still
-- cannot read is left alone.
UPDATE person_messages
SET sent_at = strftime('%Y-%m-%d %H:%M:%f',
    substr(sent_at, 1, 18 + instr(substr(sent_at, 20), ' '))
    || substr(sent_at, 20 + instr(substr(sent_at, 20), ' '), 3)
    || ':' || substr(sent_at, 23 + instr(substr(sent_at, 20), ' '), 2))
WHERE typeof(sent_at) = 'text'
  AND sent_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] [+-][0-9][0-9][0-9][0-9]*'
  AND strftime('%Y-%m-%d %H:%M:%f',
      substr(sent_at, 1, 18 + instr(substr(sent_at, 20), ' '))
      || substr(sent_at, 20 + instr(substr(sent_at, 20), ' '), 3)
      || ':' || substr(sent_at, 23 + instr(substr(sent_at, 20), ' '), 2)) IS NOT NULL;
