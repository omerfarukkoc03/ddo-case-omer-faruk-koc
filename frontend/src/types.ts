export interface AnimalOwner {
  owner_id: number;
  full_name: string;
  phone: string;
  email: string | null;
  relation: string;
  is_billing: boolean;
  has_portal_account: boolean;
}

export interface Animal {
  id: number;
  name: string;
  species: string;
  breed: string;
  sex: string;
  microchip: string | null;
  birth_date: string;
  birth_date_estimated: boolean;
  weight_kg: string | null;
  merged_into: number | null;
  owners?: AnimalOwner[];
}

export interface Owner {
  id: number;
  full_name: string;
  phone: string;
  email: string | null;
  address: string | null;
}

export interface Dose {
  id: number;
  vaccine_code: string;
  vaccine_name: string;
  dose_number: number;
  kind: string;
  due_date: string;
  status: string;
  administered_at: string | null;
  batch_no: string | null;
  package_used: boolean;
}

export interface Plan {
  vaccine_code: string;
  late_start: boolean;
  series_complete: boolean;
  estimated_age: boolean;
  ignored_doses: { date: string; reason: string }[] | null;
  planned: { number: number; kind: string; due_date: string; overdue: boolean; reason: string }[];
}

export interface Vaccinations {
  today: string;
  doses: Dose[];
  plans: Plan[];
}

export interface Appointment {
  id: number;
  animal_id: number;
  animal_name: string;
  species: string;
  vet_id: number;
  vet_name: string;
  room_id: number;
  room_name: string;
  kind: string;
  priority: string;
  starts_at: string;
  ends_at: string;
  status: string;
  reason: string | null;
  original_starts_at: string | null;
  reschedule_count: number;
}

export interface Resource {
  id: number;
  kind: string;
  name: string;
}

export interface Step {
  label: string;
  formula: string;
  result: string;
}

export interface DoseResult {
  weight_kg: string;
  mg_per_kg: string;
  dose_mg: string;
  volume_ml?: string;
  tablets?: string;
  dilution?: { target_mg_per_ml: string; stock_ml: string; diluent_ml: string; final_ml: string };
  dispense_quantity: number;
  dispense_unit: string;
  exceeds_max: boolean;
  warnings: string[];
  steps: Step[];
}

export interface Drug {
  id: number;
  name: string;
  active_ingredient: string;
  form: string;
  stock_unit: string;
  stock_on_hand: number;
  nearest_expires_on: string | null;
  dosing: Record<string, { mg_per_kg_min: string; mg_per_kg_max: string; max_single_dose_mg?: string }>;
  contraindications: { species: string; breed?: string; reason: string }[];
}

export interface Prescription {
  id: number;
  animal_id: number;
  animal_name: string;
  drug_name: string;
  vet_name: string;
  weight_kg_snapshot: string;
  mg_per_kg: string;
  dose_mg: string;
  dose_volume_ml: string | null;
  dose_tablets: string | null;
  doses_per_day: number;
  duration_days: number;
  dispense_quantity: number;
  calculation: DoseResult;
  exceeds_max: boolean;
  override_reason: string | null;
  status: string;
  created_at: string;
  weight_changed_note?: string;
}

export interface Invoice {
  id: number;
  animal_id: number;
  animal_name: string;
  owner_id: number;
  owner_name: string;
  billing_rule: string;
  status: string;
  total_kurus: number;
  paid_kurus: number;
  due_kurus: number;
  created_at: string;
  lines?: { description: string; quantity: number; unit_price_kurus: number; line_total_kurus: number }[];
  payments?: { id: number; amount_kurus: number; method: string; created_at: string }[];
}

export interface Cage {
  id: number;
  code: string;
  size: string;
  ward: string;
  state: string;
  allowed_next: string[];
  hospitalization_id: number | null;
  animal_id: number | null;
  animal_name: string | null;
  admitted_at: string | null;
  reason: string | null;
}

export interface AuditRow {
  id: number;
  at: string;
  actor_role: string | null;
  action: string;
  entity: string;
  entity_id: string;
}

export interface LiveStats {
  animals: number;
  owners: number;
  appointments_today: number;
  emergencies_today: number;
  active_inpatients: number;
  open_invoices: number;
  open_balance_kurus: number;
  vaccine_doses_due_7d: number;
  pending_duplicate_owners: number;
  events_last_minute: number;
  last_event_at: string | null;
  cages: Record<string, number>;
  recent_events: AuditRow[];
  server_time: string;
}
