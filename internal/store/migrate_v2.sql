-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Schema versione 2: invio dello schema per email come PDF protetto da password.

ALTER TABLE patients ADD COLUMN email_auto INTEGER NOT NULL DEFAULT 0;
ALTER TABLE patients ADD COLUMN pdf_password TEXT NOT NULL DEFAULT '';

-- Un tentativo di invio per riga, riuscito o fallito. Solo inserimento.
CREATE TABLE email_sends (
  id          INTEGER PRIMARY KEY,
  patient_id  INTEGER NOT NULL REFERENCES patients(id),
  document_id INTEGER NOT NULL REFERENCES printed_documents(id),
  recipient   TEXT NOT NULL,
  ok          INTEGER NOT NULL,
  error       TEXT NOT NULL DEFAULT '',
  user_id     INTEGER NOT NULL REFERENCES users(id),
  created_at  TEXT NOT NULL
) STRICT;
CREATE INDEX email_sends_patient ON email_sends(patient_id, id);
CREATE TRIGGER email_sends_no_update BEFORE UPDATE ON email_sends
BEGIN SELECT RAISE(ABORT, 'registro degli invii non modificabile'); END;
CREATE TRIGGER email_sends_no_delete BEFORE DELETE ON email_sends
BEGIN SELECT RAISE(ABORT, 'registro degli invii non cancellabile'); END;

INSERT INTO settings (key, value) VALUES
  ('email_enabled', '0'),
  ('smtp_host', ''),
  ('smtp_port', '587'),
  ('smtp_user', ''),
  ('smtp_password', ''),
  ('smtp_from', ''),
  ('smtp_from_name', ''),
  ('email_subject', 'Documento dal suo medico'),
  ('email_body', 'In allegato trova un documento in formato PDF. Il file è protetto: per aprirlo serve la password che Le è stata consegnata di persona.');
