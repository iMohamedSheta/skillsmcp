import type { SkillInput } from './types';

// Unsaved skill-sheet drafts, persisted to localStorage so closing the
// sheet (or restarting the app) never loses text. A draft is cleared only
// on save or on explicit confirmed Reset inside the sheet.
const LS_KEY = 'skillsmcp-skill-drafts-v1';

function readAll(): Record<string, SkillInput> {
  try {
    const raw = localStorage.getItem(LS_KEY);
    if (raw) {
      const m = JSON.parse(raw);
      if (m && typeof m === 'object') return m as Record<string, SkillInput>;
    }
  } catch {}
  return {};
}

function writeAll(m: Record<string, SkillInput>) {
  try {
    localStorage.setItem(LS_KEY, JSON.stringify(m));
  } catch {}
}

/** Draft key: 'new' for the new-skill sheet, `edit:<id>` per skill. */
export function draftKeyFor(editingId: string | null): string {
  return editingId ? `edit:${editingId}` : 'new';
}

export function loadDraft(key: string): SkillInput | null {
  const m = readAll();
  const d = m[key];
  if (!d || typeof d !== 'object') return null;
  // must at least carry the text fields to count as a draft
  if (typeof (d as any).content !== 'string') return null;
  return d as SkillInput;
}

export function saveDraft(key: string, form: SkillInput): void {
  const m = readAll();
  m[key] = { ...form };
  writeAll(m);
}

export function clearDraft(key: string): void {
  const m = readAll();
  if (key in m) {
    delete m[key];
    writeAll(m);
  }
}

export function hasDraft(key: string): boolean {
  return loadDraft(key) !== null;
}
