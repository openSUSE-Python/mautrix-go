-- v34 (compatible with v9+): Add column for ghost name quality
ALTER TABLE ghost ADD COLUMN name_quality INTEGER NOT NULL DEFAULT 0;
