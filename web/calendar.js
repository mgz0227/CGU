(() => {
  const i18n = window.CGU_I18N;
  const list = document.querySelector('[data-calendar-list]');
  const alert = document.querySelector('[data-calendar-alert]');
  const refresh = document.querySelector('[data-calendar-refresh]');
  const year = document.querySelector('[data-calendar-year]');
  let items = [];

  const t = (key, vars) => i18n?.t(key, vars) || key;
  const escapeHTML = (value) => String(value ?? '').replace(/[&<>'"]/g, (character) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', "'": '&#39;', '"': '&quot;' }[character]));
  const localeValue = (item, field) => {
    const suffix = i18n?.locale === 'en' ? 'En' : 'Zh';
    return String(item?.[`${field}${suffix}`] ?? item?.[field] ?? '').trim();
  };
  const dateValue = (value) => {
    const date = new Date(value || '');
    return Number.isNaN(date.getTime()) ? { day: '—', year: '' } : {
      day: new Intl.DateTimeFormat(i18n?.locale === 'en' ? 'en-US' : 'zh-CN', { month: '2-digit', day: '2-digit' }).format(date),
      year: String(date.getFullYear())
    };
  };
  const showAlert = (message) => {
    if (!alert) return;
    alert.textContent = message;
    alert.hidden = !message;
  };
  const render = () => {
    if (!list) return;
    if (!items.length) {
      list.innerHTML = `<p class="empty-state">${escapeHTML(t('calendar.empty'))}</p>`;
      return;
    }
    list.innerHTML = items.map((item) => {
      const date = dateValue(item.publishedAt || item.createdAt);
      return `<article class="calendar-item"><div class="calendar-date"><strong>${escapeHTML(date.day)}</strong><span>${escapeHTML(date.year)}</span></div><div><span class="calendar-type">${escapeHTML(String(item.type || t('calendar.notice')).toUpperCase())}</span><h2>${escapeHTML(localeValue(item, 'title'))}</h2><p>${escapeHTML(localeValue(item, 'content'))}</p></div></article>`;
    }).join('');
  };
  const load = async () => {
    if (refresh) refresh.disabled = true;
    showAlert('');
    try {
      const response = await fetch('/api/announcements', { credentials: 'same-origin', headers: { Accept: 'application/json' } });
      if (!response.ok) throw new Error('calendar unavailable');
      const payload = await response.json();
      items = (Array.isArray(payload?.announcements) ? payload.announcements : []).filter((item) => item?.published !== false).sort((a, b) => new Date(b.publishedAt || b.createdAt || 0) - new Date(a.publishedAt || a.createdAt || 0));
      render();
    } catch {
      items = [];
      if (list) list.innerHTML = `<p class="empty-state">${escapeHTML(t('calendar.error'))}</p>`;
      showAlert(t('calendar.error'));
    } finally {
      if (refresh) refresh.disabled = false;
    }
  };
  if (year) year.textContent = String(new Date().getFullYear());
  refresh?.addEventListener('click', load);
  window.addEventListener('cgu:localechange', render);
  Promise.resolve(i18n?.ready).catch(() => {}).finally(load);
})();
