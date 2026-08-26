(() => {
  const i18n = window.CGU_I18N;
  const list = document.querySelector('[data-catalog-list]');
  const search = document.querySelector('[data-catalog-search]');
  const term = document.querySelector('[data-catalog-term]');
  const alert = document.querySelector('[data-catalog-alert]');
  let courses = [];
  const t = (key) => i18n?.t(key) || key;
  const escapeHTML = (value) => String(value ?? '').replace(/[&<>'"]/g, (character) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', "'": '&#39;', '"': '&quot;' }[character]));
  const localized = (item, base) => {
    const suffix = i18n?.locale === 'en' ? 'En' : 'Zh';
    return String(item?.[`${base}${suffix}`] ?? item?.[base] ?? '').trim();
  };
  const renderTerms = () => {
    if (!term) return;
    const current = term.value || 'all';
    const values = [...new Set(courses.map((course) => localized(course, 'term')).filter(Boolean))].sort();
    term.innerHTML = `<option value="all">${escapeHTML(t('catalog.allTerms'))}</option>${values.map((value) => `<option value="${escapeHTML(value)}">${escapeHTML(value)}</option>`).join('')}`;
    term.value = values.includes(current) ? current : 'all';
  };
  const render = () => {
    if (!list) return;
    const query = String(search?.value || '').trim().toLocaleLowerCase();
    const selectedTerm = term?.value || 'all';
    const filtered = courses.filter((course) => {
      const haystack = [course.code, localized(course, 'name'), localized(course, 'department'), localized(course, 'teacher'), localized(course, 'description')].join(' ').toLocaleLowerCase();
      return (!query || haystack.includes(query)) && (selectedTerm === 'all' || localized(course, 'term') === selectedTerm);
    });
    list.innerHTML = filtered.length ? filtered.map((course) => `<tr><td><strong>${escapeHTML(course.code || '—')}</strong></td><td><strong>${escapeHTML(localized(course, 'name') || '—')}</strong><small>${escapeHTML(localized(course, 'description'))}</small></td><td>${escapeHTML(localized(course, 'department') || '—')}</td><td>${escapeHTML(localized(course, 'teacher') || '—')}</td><td>${escapeHTML(course.credits ?? '—')}</td><td>${escapeHTML(localized(course, 'term') || '—')}</td></tr>`).join('') : `<tr><td colspan="6" class="empty-state">${escapeHTML(t('catalog.empty'))}</td></tr>`;
  };
  const load = async () => {
    try {
      const response = await fetch('/api/courses', { credentials: 'same-origin', headers: { Accept: 'application/json' } });
      if (!response.ok) throw new Error('catalog unavailable');
      const payload = await response.json();
      courses = Array.isArray(payload?.courses) ? payload.courses : [];
      renderTerms();
      render();
    } catch {
      courses = [];
      if (list) list.innerHTML = `<tr><td colspan="6" class="empty-state">${escapeHTML(t('catalog.error'))}</td></tr>`;
      if (alert) { alert.textContent = t('catalog.error'); alert.hidden = false; }
    }
  };
  search?.addEventListener('input', render);
  term?.addEventListener('change', render);
  window.addEventListener('cgu:localechange', () => { renderTerms(); render(); });
  const year = document.querySelector('[data-catalog-year]');
  if (year) year.textContent = String(new Date().getFullYear());
  Promise.resolve(i18n?.ready).catch(() => {}).finally(load);
})();
