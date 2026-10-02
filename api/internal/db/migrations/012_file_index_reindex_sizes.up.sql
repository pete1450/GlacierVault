-- File index rows built from `rustic ls --json` (paths only) carry no sizes
-- or mtimes. Listing now uses `rustic ls --long`; clear the index so each
-- snapshot is re-indexed with sizes on next browse (one-time, with progress).
DELETE FROM file_index;
