-- 107_person_messages_sent_at_utc.sql
-- A message's sent_at given in a zone other than UTC (`people message add
-- --sent-at` or an import with an RFC3339 offset) was stored as Go's time
-- text, "2026-10-19 12:00:00 +0330 +0330", or with the local zone's name,
-- "2026-10-19 08:00:00 -0400 EDT". The driver cannot read the numeric zone
-- name back, so listing that person's messages failed, and as text both
-- ordered by local time. sent_at is now written in UTC; this rewrites the
-- stored ones to the same instant in UTC ("2026-10-19 08:30:00"), keeping
-- the fraction of a second as written (an offset is whole minutes).
--
-- k is the position, after the 19 characters of date and time, of the space
-- before the "±hhmm" offset, and frac what lies between: "" or ".ddd".
-- Values already at +0000, values in any other form (RFC3339, unix seconds,
-- empty, NULL) and a value SQLite cannot read are left as they are. A
-- rewritten value no longer matches, so running this again changes nothing.
UPDATE person_messages AS pm
SET sent_at = v.utc || v.frac
FROM (
    SELECT id, frac,
        strftime('%Y-%m-%d %H:%M:%S', substr(sent_at, 1, 19)
            || substr(sent_at, 20 + k, 3) || ':' || substr(sent_at, 23 + k, 2)) AS utc
    FROM (
        SELECT id, sent_at, k, substr(sent_at, 20, k - 1) AS frac
        FROM (
            SELECT id, sent_at, instr(substr(sent_at, 20), ' ') AS k
            FROM person_messages
            WHERE typeof(sent_at) = 'text'
              AND sent_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] ?*'
              AND sent_at NOT GLOB '* +0000 *'
        )
    )
    WHERE frac = '' OR (frac GLOB '.[0-9]*' AND substr(frac, 2) NOT GLOB '*[^0-9]*')
) AS v
WHERE pm.id = v.id AND v.utc IS NOT NULL;
