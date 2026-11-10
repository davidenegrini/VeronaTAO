-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Schema VeronaTAO, versione 1.
-- Le date cliniche sono TEXT nel formato AAAA-MM-GG, senza ora e senza fuso.
-- Le dosi di anticoagulante orale sono interi in quarti di compressa.
-- L'INR è un intero in centesimi; i mg sono interi in millesimi.

CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
) STRICT;

CREATE TABLE users (
  id              INTEGER PRIMARY KEY,
  username        TEXT NOT NULL UNIQUE COLLATE NOCASE,
  full_name       TEXT NOT NULL,
  password_hash   TEXT NOT NULL,
  role            TEXT NOT NULL CHECK (role IN ('admin','clinician','assistant')),
  active          INTEGER NOT NULL DEFAULT 1,
  must_change     INTEGER NOT NULL DEFAULT 0,
  failed_attempts INTEGER NOT NULL DEFAULT 0,
  locked_until    TEXT NOT NULL DEFAULT '',
  last_login_at   TEXT NOT NULL DEFAULT '',
  created_at      TEXT NOT NULL
) STRICT;

CREATE TABLE drugs (
  id            INTEGER PRIMARY KEY,
  ingredient    TEXT NOT NULL,
  brand         TEXT NOT NULL,
  mg_milli      INTEGER NOT NULL CHECK (mg_milli > 0 AND mg_milli % 4 = 0),
  divisibility  TEXT NOT NULL CHECK (divisibility IN ('quarter','half','whole')),
  active        INTEGER NOT NULL DEFAULT 1
) STRICT;

CREATE TABLE indications (
  id         INTEGER PRIMARY KEY,
  name       TEXT NOT NULL,
  inr_min    INTEGER NOT NULL DEFAULT 0,
  inr_max    INTEGER NOT NULL DEFAULT 0,
  inr_target INTEGER NOT NULL DEFAULT 0,
  active     INTEGER NOT NULL DEFAULT 1
) STRICT;

CREATE TABLE event_types (
  id       INTEGER PRIMARY KEY,
  name     TEXT NOT NULL,
  category TEXT NOT NULL CHECK (category IN ('hemorrhagic','thrombotic','other')),
  active   INTEGER NOT NULL DEFAULT 1
) STRICT;

CREATE TABLE patients (
  id            INTEGER PRIMARY KEY,
  last_name     TEXT NOT NULL COLLATE NOCASE,
  first_name    TEXT NOT NULL COLLATE NOCASE,
  birth_date    TEXT NOT NULL,
  sex           TEXT NOT NULL DEFAULT '' CHECK (sex IN ('','M','F')),
  fiscal_code   TEXT NOT NULL DEFAULT '',
  phone         TEXT NOT NULL DEFAULT '',
  email         TEXT NOT NULL DEFAULT '',
  address       TEXT NOT NULL DEFAULT '',
  external_code TEXT NOT NULL DEFAULT '',
  notes         TEXT NOT NULL DEFAULT '',
  status        TEXT NOT NULL DEFAULT 'active'
                CHECK (status IN ('active','suspended','transferred','deceased','lost_to_followup')),
  created_by    INTEGER NOT NULL REFERENCES users(id),
  created_at    TEXT NOT NULL,
  updated_at    TEXT NOT NULL,
  deleted_at    TEXT NOT NULL DEFAULT '',
  delete_reason TEXT NOT NULL DEFAULT ''
) STRICT;
CREATE INDEX patients_name ON patients(last_name, first_name);

CREATE TABLE patient_status_changes (
  id         INTEGER PRIMARY KEY,
  patient_id INTEGER NOT NULL REFERENCES patients(id),
  old_status TEXT NOT NULL,
  new_status TEXT NOT NULL,
  date       TEXT NOT NULL,
  reason     TEXT NOT NULL DEFAULT '',
  user_id    INTEGER NOT NULL REFERENCES users(id),
  created_at TEXT NOT NULL
) STRICT;

-- Percorsi di trattamento: un paziente può averne più d'uno nel tempo,
-- ma al massimo uno aperto.
CREATE TABLE treatments (
  id            INTEGER PRIMARY KEY,
  patient_id    INTEGER NOT NULL REFERENCES patients(id),
  indication_id INTEGER NOT NULL REFERENCES indications(id),
  diagnosis     TEXT NOT NULL DEFAULT '',
  drug_id       INTEGER NOT NULL REFERENCES drugs(id),
  opened_at     TEXT NOT NULL,
  closed_at     TEXT NOT NULL DEFAULT '',
  sine_die      INTEGER NOT NULL DEFAULT 1,
  planned_end   TEXT NOT NULL DEFAULT '',
  close_reason  TEXT NOT NULL DEFAULT '',
  close_notes   TEXT NOT NULL DEFAULT '',
  created_by    INTEGER NOT NULL REFERENCES users(id),
  created_at    TEXT NOT NULL
) STRICT;
CREATE UNIQUE INDEX treatments_one_open ON treatments(patient_id) WHERE closed_at = '';

CREATE TABLE target_periods (
  id           INTEGER PRIMARY KEY,
  treatment_id INTEGER NOT NULL REFERENCES treatments(id),
  valid_from   TEXT NOT NULL,
  valid_to     TEXT NOT NULL DEFAULT '',
  inr_min      INTEGER NOT NULL,
  inr_max      INTEGER NOT NULL,
  inr_target   INTEGER NOT NULL DEFAULT 0,
  reason       TEXT NOT NULL DEFAULT '',
  user_id      INTEGER NOT NULL REFERENCES users(id),
  created_at   TEXT NOT NULL,
  CHECK (inr_min > 0 AND inr_max >= inr_min)
) STRICT;
CREATE UNIQUE INDEX target_one_current ON target_periods(treatment_id) WHERE valid_to = '';

-- Un controllo: misurazione INR (facoltativa) e piano posologico che ne
-- scaturisce (facoltativo, presente se drug_id non è NULL).
CREATE TABLE controls (
  id            INTEGER PRIMARY KEY,
  patient_id    INTEGER NOT NULL REFERENCES patients(id),
  treatment_id  INTEGER NOT NULL REFERENCES treatments(id),
  date          TEXT NOT NULL,
  time          TEXT NOT NULL DEFAULT '',
  inr           INTEGER CHECK (inr IS NULL OR inr > 0),
  site          TEXT NOT NULL DEFAULT 'lab' CHECK (site IN ('lab','poct','home')),
  drug_id       INTEGER REFERENCES drugs(id),
  q_mon INTEGER NOT NULL DEFAULT 0 CHECK (q_mon >= 0),
  q_tue INTEGER NOT NULL DEFAULT 0 CHECK (q_tue >= 0),
  q_wed INTEGER NOT NULL DEFAULT 0 CHECK (q_wed >= 0),
  q_thu INTEGER NOT NULL DEFAULT 0 CHECK (q_thu >= 0),
  q_fri INTEGER NOT NULL DEFAULT 0 CHECK (q_fri >= 0),
  q_sat INTEGER NOT NULL DEFAULT 0 CHECK (q_sat >= 0),
  q_sun INTEGER NOT NULL DEFAULT 0 CHECK (q_sun >= 0),
  first_day_q   INTEGER CHECK (first_day_q IS NULL OR first_day_q >= 0),
  plan_end      TEXT NOT NULL DEFAULT '',
  next_date     TEXT NOT NULL DEFAULT '',
  dose_source   TEXT NOT NULL DEFAULT 'manual' CHECK (dose_source IN ('manual','copied_from_previous')),
  comment       TEXT NOT NULL DEFAULT '',
  user_id       INTEGER NOT NULL REFERENCES users(id),
  created_at    TEXT NOT NULL,
  deleted_at    TEXT NOT NULL DEFAULT '',
  delete_reason TEXT NOT NULL DEFAULT '',
  CHECK (drug_id IS NULL OR plan_end <> '')
) STRICT;
CREATE INDEX controls_patient ON controls(patient_id, date);

-- Una riga per ogni singolo giorno del piano. quarters = 0 significa
-- "oggi nessuna compressa" ed è diverso dall'assenza di riga.
-- superseded_by: controllo successivo che ha sostituito questo giorno
-- (NULL = giorno in vigore).
CREATE TABLE plan_days (
  id            INTEGER PRIMARY KEY,
  control_id    INTEGER NOT NULL REFERENCES controls(id),
  patient_id    INTEGER NOT NULL REFERENCES patients(id),
  date          TEXT NOT NULL,
  quarters      INTEGER NOT NULL CHECK (quarters >= 0),
  hep_drug      TEXT NOT NULL DEFAULT '',
  hep_dose      TEXT NOT NULL DEFAULT '',
  hep_unit      TEXT NOT NULL DEFAULT '' CHECK (hep_unit IN ('','mg','UI')),
  hep_times     INTEGER NOT NULL DEFAULT 0 CHECK (hep_times >= 0),
  superseded_by INTEGER REFERENCES controls(id),
  UNIQUE (control_id, date)
) STRICT;
-- Al massimo un giorno in vigore per paziente e data: i piani non si sovrappongono.
CREATE UNIQUE INDEX plan_days_one_active ON plan_days(patient_id, date) WHERE superseded_by IS NULL;

CREATE TABLE medications (
  id            INTEGER PRIMARY KEY,
  patient_id    INTEGER NOT NULL REFERENCES patients(id),
  name          TEXT NOT NULL,
  dosage        TEXT NOT NULL DEFAULT '',
  start_date    TEXT NOT NULL,
  end_date      TEXT NOT NULL DEFAULT '',
  interfering   INTEGER NOT NULL DEFAULT 0,
  direction     TEXT NOT NULL DEFAULT '' CHECK (direction IN ('','increases_inr','decreases_inr','undefined')),
  notes         TEXT NOT NULL DEFAULT '',
  user_id       INTEGER NOT NULL REFERENCES users(id),
  created_at    TEXT NOT NULL,
  deleted_at    TEXT NOT NULL DEFAULT '',
  delete_reason TEXT NOT NULL DEFAULT '',
  CHECK ((interfering = 1 AND direction <> '') OR (interfering = 0 AND direction = ''))
) STRICT;

CREATE TABLE events (
  id            INTEGER PRIMARY KEY,
  patient_id    INTEGER NOT NULL REFERENCES patients(id),
  date          TEXT NOT NULL,
  type_id       INTEGER NOT NULL REFERENCES event_types(id),
  description   TEXT NOT NULL DEFAULT '',
  outcome       TEXT NOT NULL DEFAULT '',
  user_id       INTEGER NOT NULL REFERENCES users(id),
  created_at    TEXT NOT NULL,
  deleted_at    TEXT NOT NULL DEFAULT '',
  delete_reason TEXT NOT NULL DEFAULT ''
) STRICT;

CREATE TABLE printed_documents (
  id            INTEGER PRIMARY KEY,
  patient_id    INTEGER NOT NULL REFERENCES patients(id),
  control_id    INTEGER NOT NULL REFERENCES controls(id),
  sha256        TEXT NOT NULL,
  period_from   TEXT NOT NULL,
  period_to     TEXT NOT NULL,
  template_code TEXT NOT NULL,
  pdf           BLOB NOT NULL,
  user_id       INTEGER NOT NULL REFERENCES users(id),
  created_at    TEXT NOT NULL
) STRICT;

-- Registro delle attività: solo inserimento.
CREATE TABLE audit_log (
  id         INTEGER PRIMARY KEY,
  at         TEXT NOT NULL,
  user_id    INTEGER NOT NULL DEFAULT 0,
  username   TEXT NOT NULL DEFAULT '',
  action     TEXT NOT NULL,
  entity     TEXT NOT NULL DEFAULT '',
  entity_id  INTEGER NOT NULL DEFAULT 0,
  patient_id INTEGER NOT NULL DEFAULT 0,
  details    TEXT NOT NULL DEFAULT '',
  ip         TEXT NOT NULL DEFAULT ''
) STRICT;
CREATE INDEX audit_patient ON audit_log(patient_id, id);

CREATE TRIGGER audit_log_no_update BEFORE UPDATE ON audit_log
BEGIN SELECT RAISE(ABORT, 'audit_log non modificabile'); END;
CREATE TRIGGER audit_log_no_delete BEFORE DELETE ON audit_log
BEGIN SELECT RAISE(ABORT, 'audit_log non cancellabile'); END;

-- I documenti stampati sono immutabili.
CREATE TRIGGER printed_no_update BEFORE UPDATE ON printed_documents
BEGIN SELECT RAISE(ABORT, 'documento stampato non modificabile'); END;
CREATE TRIGGER printed_no_delete BEFORE DELETE ON printed_documents
BEGIN SELECT RAISE(ABORT, 'documento stampato non cancellabile'); END;

-- Dati iniziali di comodo, modificabili dall'amministratore.
INSERT INTO drugs (ingredient, brand, mg_milli, divisibility) VALUES
  ('Warfarin sodico', 'Coumadin', 5000, 'quarter'),
  ('Acenocumarolo', 'Sintrom', 4000, 'quarter'),
  ('Acenocumarolo', 'Sintrom', 1000, 'whole');

INSERT INTO indications (name, inr_min, inr_max, inr_target) VALUES
  ('Fibrillazione atriale non valvolare', 200, 300, 250),
  ('Protesi valvolare meccanica aortica', 200, 300, 250),
  ('Protesi valvolare meccanica mitralica', 250, 350, 300),
  ('Protesi valvolare biologica', 200, 300, 250),
  ('Tromboembolismo venoso / TVP', 200, 300, 250),
  ('Embolia polmonare', 200, 300, 250),
  ('Sindrome da anticorpi antifosfolipidi', 200, 300, 250),
  ('Altro', 200, 300, 250);

INSERT INTO event_types (name, category) VALUES
  ('Sanguinamento minore', 'hemorrhagic'),
  ('Sanguinamento maggiore', 'hemorrhagic'),
  ('Evento tromboembolico venoso', 'thrombotic'),
  ('Evento tromboembolico arterioso', 'thrombotic'),
  ('Ricovero', 'other'),
  ('Procedura invasiva', 'other'),
  ('Sospensione della terapia', 'other'),
  ('Altro', 'other');

INSERT INTO settings (key, value) VALUES
  ('header', 'Studio medico'),
  ('doc_title', 'Scheda personale per il dosaggio della terapia anticoagulante'),
  ('template_code', 'VTAO-01 - Rev. 1'),
  ('footer_text', ''),
  ('print_ttr', '1'),
  ('session_minutes', '20'),
  ('max_daily_quarters', '16'),
  ('weekly_variation_pct', '30'),
  ('lost_days', '90'),
  ('due_days', '7'),
  ('backup_warn_days', '7'),
  ('backup_keep', '30'),
  ('backup_passphrase', ''),
  ('last_backup_at', ''),
  ('last_verify_at', ''),
  ('lan_enabled', '0'),
  ('port', '8756');
