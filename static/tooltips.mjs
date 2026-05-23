// Lightweight popover for metric hint glyphs (the ⓘ next to a label).
//
// Why a shared overlay instead of a CSS `::after` per hint?
// The metric strip uses narrow grid columns and the gauge sub-stats sit
// inside a tight flex row — a CSS-only tooltip positioned relative to its
// trigger would clip against the column edge on phones. A single shared
// element positioned in viewport coordinates lets us clamp to the screen
// in one place, so a 200-character message can't run off the right edge
// no matter which metric it belongs to.
//
// Triggers:
//   - mouseenter / focus (desktop, keyboard, screen reader)
//   - tap   on touch (click toggles; tapping outside dismisses)
//
// Each trigger declares its message via `data-tooltip-key`; the i18n
// layer in app.js calls `refreshTooltips()` after `lang` changes to swap
// the resolved strings into `data-tooltip` on the same elements.

const VIEWPORT_MARGIN = 8;      // keep at least this many px from screen edges
const SHOW_DELAY_MS   = 60;     // small delay avoids flicker on hover-through

let overlay = null;
let activeTrigger = null;
let showTimer = 0;

function ensureOverlay() {
  if (overlay) return overlay;
  overlay = document.createElement('div');
  overlay.id = 'tooltip-overlay';
  overlay.setAttribute('role', 'tooltip');
  overlay.hidden = true;
  document.body.appendChild(overlay);
  return overlay;
}

function positionOverlay(trigger) {
  const el = ensureOverlay();
  const rect = trigger.getBoundingClientRect();
  // Render first so we can measure, then clamp into the viewport.
  el.hidden = false;
  const tipRect = el.getBoundingClientRect();
  const vw = window.innerWidth;
  const vh = window.innerHeight;

  // Prefer above the trigger; fall back below if there isn't room.
  let top = rect.top - tipRect.height - 8;
  if (top < VIEWPORT_MARGIN) top = rect.bottom + 8;
  // Center horizontally on the trigger, clamped to viewport.
  const centeredLeft = rect.left + rect.width / 2 - tipRect.width / 2;
  const maxLeft = vw - tipRect.width - VIEWPORT_MARGIN;
  const left = Math.max(VIEWPORT_MARGIN, Math.min(centeredLeft, maxLeft));
  // Clamp top to viewport too, in case there's no room above or below.
  const clampedTop = Math.max(
    VIEWPORT_MARGIN,
    Math.min(top, vh - tipRect.height - VIEWPORT_MARGIN),
  );

  el.style.left = `${Math.round(left)}px`;
  el.style.top  = `${Math.round(clampedTop)}px`;
}

function showTooltip(trigger) {
  const msg = trigger.getAttribute('data-tooltip');
  if (!msg) return;
  const el = ensureOverlay();
  el.textContent = msg;
  activeTrigger = trigger;
  positionOverlay(trigger);
}

function hideTooltip() {
  clearTimeout(showTimer);
  showTimer = 0;
  if (!overlay) return;
  overlay.hidden = true;
  activeTrigger = null;
}

function scheduleShow(trigger) {
  clearTimeout(showTimer);
  showTimer = setTimeout(() => showTooltip(trigger), SHOW_DELAY_MS);
}

/**
 * Wire up tooltip triggers in the document. Idempotent — safe to call
 * more than once (existing handlers are not duplicated because the
 * delegated listeners live on document, not on individual triggers).
 */
export function mountTooltips() {
  ensureOverlay();

  document.addEventListener('mouseover', e => {
    const trigger = e.target.closest('.hint[data-tooltip]');
    if (trigger) scheduleShow(trigger);
  });
  document.addEventListener('mouseout', e => {
    const trigger = e.target.closest('.hint[data-tooltip]');
    if (!trigger) return;
    // Don't hide if focus is still on the trigger (keyboard nav).
    if (document.activeElement === trigger) return;
    hideTooltip();
  });

  document.addEventListener('focusin', e => {
    const trigger = e.target.closest('.hint[data-tooltip]');
    if (trigger) showTooltip(trigger);
  });
  document.addEventListener('focusout', e => {
    const trigger = e.target.closest('.hint[data-tooltip]');
    if (trigger) hideTooltip();
  });

  // Touch / mouse click: tap-to-toggle. Click outside dismisses.
  document.addEventListener('click', e => {
    const trigger = e.target.closest('.hint[data-tooltip]');
    if (!trigger) {
      hideTooltip();
      return;
    }
    if (activeTrigger === trigger) hideTooltip();
    else showTooltip(trigger);
    e.stopPropagation();
  });

  // Repositioning during scroll/resize keeps the popover anchored to
  // its trigger if the user scrolls while it's open.
  window.addEventListener('scroll', () => {
    if (activeTrigger) positionOverlay(activeTrigger);
  }, { passive: true });
  window.addEventListener('resize', () => {
    if (activeTrigger) positionOverlay(activeTrigger);
  });

  // Esc dismisses for keyboard users.
  document.addEventListener('keydown', e => {
    if (e.key === 'Escape') hideTooltip();
  });
}

/**
 * Copy resolved tooltip text from a translator function into the DOM.
 * Called from app.js whenever the active language changes so the popover
 * shows the right language without ever holding stale strings.
 *
 * @param {(key: string) => string} translate  resolves an i18n key to its string
 */
export function refreshTooltips(translate) {
  document.querySelectorAll('.hint[data-tooltip-key]').forEach(el => {
    const key = el.getAttribute('data-tooltip-key');
    const txt = translate(key);
    if (txt) {
      el.setAttribute('data-tooltip', txt);
      el.setAttribute('aria-label', txt);
    }
  });
  // If a tooltip is currently open, refresh its text in place.
  if (activeTrigger && overlay && !overlay.hidden) {
    showTooltip(activeTrigger);
  }
}
