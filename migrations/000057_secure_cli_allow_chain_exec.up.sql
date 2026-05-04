ALTER TABLE secure_cli_binaries ADD COLUMN IF NOT EXISTS allow_chain_exec BOOLEAN NOT NULL DEFAULT false;
