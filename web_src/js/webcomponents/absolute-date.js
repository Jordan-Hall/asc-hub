export function toAbsoluteLocaleDate(date, lang, opts) {
  // only use the date part, it is guaranteed to be in ISO format (YYYY-MM-DDTHH:mm:ss.sssZ) or (YYYY-MM-DD)
  // if there is an "Invalid Date" error, there must be something wrong in code and should be fixed.
  // TODO: there is a root problem in backend code: the date "YYYY-MM-DD" is passed to backend without timezone (eg: deadline),
  // then backend parses it in server's timezone and stores the parsed timestamp into database.
  // If the user's timezone is different from the server's, the date might be displayed in the wrong day.
  const dateSep = date.indexOf('T');
  date = dateSep === -1 ? date : date.substring(0, dateSep);
  return new Date(`${date}T00:00:00`).toLocaleString(lang || [], opts);
}

// The date order the person chose on Settings, Appearance (<html data-date-order>): day first reads as British English,
// month first as American English, year first as 2026-02-03. Without it the language of the page decides.
const ORDER_LOCALES = {dmy: 'en-GB', mdy: 'en-US'};

export function toOrderedDate(date, lang, opts, order) {
  if (order !== 'ymd') return toAbsoluteLocaleDate(date, ORDER_LOCALES[order] ?? lang, opts);
  const dateSep = date.indexOf('T');
  const day = dateSep === -1 ? date : date.substring(0, dateSep);
  const parsed = new Date(`${day}T00:00:00`);
  const ymd = [parsed.getFullYear(), String(parsed.getMonth() + 1).padStart(2, '0'), String(parsed.getDate()).padStart(2, '0')].join('-');
  if (!opts.weekday) return ymd;
  return `${parsed.toLocaleString(lang || [], {weekday: opts.weekday})} ${ymd}`;
}

window.customElements.define('absolute-date', class extends HTMLElement {
  static observedAttributes = ['date', 'year', 'month', 'weekday', 'day'];

  initialized = false;
  contentSpan = null;

  update = () => {
    const opt = {};
    for (const attr of ['year', 'month', 'weekday', 'day']) {
      if (this.getAttribute(attr)) opt[attr] = this.getAttribute(attr);
    }
    const lang = this.closest('[lang]')?.getAttribute('lang') ||
      this.ownerDocument.documentElement.getAttribute('lang') || '';

    if (!this.shadowRoot) {
      this.attachShadow({mode: 'open'});
      this.contentSpan = document.createElement('span');
      this.contentSpan.setAttribute('part', 'absolute-date');
      this.shadowRoot.append(this.contentSpan);
    }
    const order = this.ownerDocument.documentElement.getAttribute('data-date-order');
    this.contentSpan.textContent = order ? toOrderedDate(this.getAttribute('date'), lang, opt, order) : toAbsoluteLocaleDate(this.getAttribute('date'), lang, opt);
  };

  attributeChangedCallback(_name, oldValue, newValue) {
    if (!this.initialized || oldValue === newValue) return;
    this.update();
  }

  connectedCallback() {
    this.initialized = false;
    this.update();
    this.initialized = true;
  }
});
