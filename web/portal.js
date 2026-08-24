(() => {
  'use strict';

  const page = document.body?.dataset.page;
  const I18N = window.CGU_I18N || {
    locale: 'zh',
    t: (key) => key,
    pick: (value, fallback = '') => value ?? fallback,
    apply: () => {},
    ready: Promise.resolve(),
    mergeSiteContent: () => {}
  };
  const API_BASE = String(window.CGU_CONFIG?.apiBase || '/api').replace(/\/$/, '');
  const USER_KEY = 'cgu_user';

  const state = {
    user: null,
    courses: [],
    enrollments: [],
    grades: [],
    schedule: [],
    announcements: [],
    adminCourses: [],
    adminAnnouncements: [],
    adminSiteContent: [],
    adminStats: {},
    toastTimer: 0
  };

  class ApiError extends Error {
    constructor(message, status = 0, code = '') {
      super(message);
      this.name = 'ApiError';
      this.status = status;
      this.code = code;
      this.network = status === 0;
    }
  }

  const readStorage = (key) => {
    try { return window.localStorage?.getItem(key) || ''; } catch { return ''; }
  };
  const writeStorage = (key, value) => {
    try {
      if (value == null || value === '') window.localStorage?.removeItem(key);
      else window.localStorage?.setItem(key, value);
    } catch { /* storage is optional */ }
  };
  const saveUser = (user) => writeStorage(USER_KEY, JSON.stringify(user));
  const readUser = () => {
    try { return JSON.parse(readStorage(USER_KEY) || 'null'); } catch { return null; }
  };
  const clearSession = () => writeStorage(USER_KEY, '');

  const jsonHeaders = () => {
    return { Accept: 'application/json', 'Content-Type': 'application/json', 'X-CGU-Request': '1' };
  };

  const api = async (path, options = {}) => {
    const request = { credentials: 'include', ...options, headers: { ...jsonHeaders(), ...(options.headers || {}) } };
    let response;
    try {
      response = await fetch(`${API_BASE}${path}`, request);
    } catch (error) {
      throw new ApiError(error?.message || 'Network error');
    }
    let payload = null;
    const text = await response.text();
    if (text) {
      try { payload = JSON.parse(text); } catch { payload = { data: text }; }
    }
    const errorValue = payload?.error;
    const errorMessage = typeof errorValue === 'string' ? (payload?.message || errorValue) : errorValue?.message;
    if (!response.ok || payload?.ok === false) {
      throw new ApiError(errorMessage || response.statusText || I18N.t('common.error'), response.status, errorValue?.code || '');
    }
    return payload?.data ?? payload ?? {};
  };

  const apiAny = async (paths, options = {}) => {
    let lastError;
    for (const path of paths) {
      try { return await api(path, options); }
      catch (error) {
        lastError = error;
        if (error.status !== 404 && error.status !== 405) throw error;
      }
    }
    throw lastError || new ApiError('API endpoint not found', 404);
  };

  const postJSON = (path, method, body) => api(path, { method, body: JSON.stringify(body) });
  const isAuthError = (error) => error?.status === 401 || error?.status === 403;

  const escapeHTML = (value) => String(value ?? '').replace(/[&<>"']/g, (character) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[character]));
  const localeValue = (value, fallback = '') => I18N.pick(value, fallback);
  const translated = (item, base, fallback = '') => localeValue(item?.[base] || item?.[`${base}Zh`] && { zh: item[`${base}Zh`], en: item[`${base}En`] }, fallback);
  const numberValue = (value, fallback = 0) => Number.isFinite(Number(value)) ? Number(value) : fallback;
  const formatDate = (value, withTime = false) => {
    if (!value) return '—';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return escapeHTML(value);
    return new Intl.DateTimeFormat(I18N.locale === 'zh' ? 'zh-CN' : 'en-US', withTime ? { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' } : { year: 'numeric', month: '2-digit', day: '2-digit' }).format(date);
  };
  const shortDate = (value) => {
    if (!value) return '—';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return '—';
    return `${String(date.getMonth() + 1).padStart(2, '0')}.${String(date.getDate()).padStart(2, '0')}`;
  };
  const listFrom = (value, keys = []) => {
    if (Array.isArray(value)) return value;
    if (!value || typeof value !== 'object') return [];
    for (const key of keys) if (Array.isArray(value[key])) return value[key];
    if (Array.isArray(value.items)) return value.items;
    return [];
  };

  const normalizeUser = (value = {}) => {
    const user = value.user || value.account || value;
    const role = String(user.role || user.userRole || (user.isAdmin ? 'admin' : 'student')).toLowerCase();
    return {
      ...user,
      id: user.id ?? user.userId ?? user.studentId ?? user.username,
      username: user.username ?? user.account ?? user.email ?? '',
      name: user.name ?? user.displayName ?? user.realName ?? user.username ?? user.account ?? 'Traveler',
      role: role === 'administrator' || role === 'staff' ? 'admin' : role,
      studentId: user.studentId ?? user.student_id ?? user.id ?? '—',
      email: user.email ?? '—',
      college: user.college ?? user.school ?? user.department ?? '—',
      year: user.year ?? user.grade ?? user.enrollmentYear ?? '—'
    };
  };

  const normalizeCourse = (value = {}) => {
    const id = value.id ?? value.courseId ?? value.course_id ?? value.code;
    const nameZh = value.nameZh ?? value.name_zh ?? value.titleZh ?? value.title_zh ?? value.name ?? value.title ?? value.code ?? id;
    const nameEn = value.nameEn ?? value.name_en ?? value.titleEn ?? value.title_en ?? nameZh;
    const enrolled = Boolean(value.enrolled ?? value.isEnrolled ?? (value.enrollmentStatus === 'enrolled'));
    return {
      ...value,
      id,
      code: value.code ?? value.courseCode ?? id,
      nameZh,
      nameEn,
      teacher: value.teacher ?? value.instructor ?? value.teacherName ?? '—',
      credits: numberValue(value.credits ?? value.credit, 0),
      term: value.term ?? value.semester ?? value.period ?? '—',
      type: String(value.type ?? value.courseType ?? 'elective').toLowerCase(),
      capacity: numberValue(value.capacity ?? value.maxCapacity, 0),
      enrolledCount: numberValue(value.enrolledCount ?? value.enrolled_count ?? value.enrollmentCount, 0),
      enrolled,
      description: value.description ?? value.summary ?? ''
    };
  };

  const normalizeGrade = (value = {}) => ({
    ...value,
    id: value.id ?? value.gradeId ?? `${value.courseId || value.course_code || Math.random()}`,
    courseId: value.courseId ?? value.course_id,
    courseCode: value.courseCode ?? value.course_code ?? value.code ?? '—',
    courseNameZh: value.courseNameZh ?? value.course_name_zh ?? value.courseName ?? value.course_name ?? value.name ?? '—',
    courseNameEn: value.courseNameEn ?? value.course_name_en ?? value.courseName ?? value.course_name ?? value.name ?? '—',
    score: value.score ?? value.mark ?? value.result ?? '—',
    point: value.point ?? value.gpa ?? value.gradePoint ?? '—',
    term: value.term ?? value.semester ?? '—',
    status: String(value.status ?? (value.score == null ? 'inProgress' : 'passed'))
  });

  const normalizeSchedule = (value = {}) => {
    let day = value.day ?? value.weekday ?? value.dayOfWeek ?? value.week ?? 1;
    if (typeof day === 'string') {
      const map = { mon: 1, monday: 1, tue: 2, tuesday: 2, wed: 3, wednesday: 3, thu: 4, thursday: 4, fri: 5, friday: 5, sat: 6, saturday: 6, sun: 7, sunday: 7 };
      day = map[day.toLowerCase()] ?? (Number.parseInt(day, 10) || 1);
    }
    return {
      ...value,
      id: value.id ?? `${value.courseId || value.course_id || value.courseCode}-${day}-${value.start || value.startTime}`,
      day: Math.min(7, Math.max(1, numberValue(day, 1))),
      start: value.start ?? value.startTime ?? value.start_time ?? '09:00',
      end: value.end ?? value.endTime ?? value.end_time ?? '10:40',
      courseId: value.courseId ?? value.course_id,
      courseCode: value.courseCode ?? value.course_code ?? value.code ?? '',
      courseNameZh: value.courseNameZh ?? value.course_name_zh ?? value.courseName ?? value.course_name ?? value.name ?? '—',
      courseNameEn: value.courseNameEn ?? value.course_name_en ?? value.courseName ?? value.course_name ?? value.name ?? '—',
      location: value.location ?? value.room ?? value.classroom ?? ''
    };
  };

  const normalizeAnnouncement = (value = {}) => ({
    ...value,
    id: value.id ?? value.announcementId ?? value.announcement_id ?? `${value.title || value.titleZh || Math.random()}`,
    type: String(value.type ?? value.category ?? 'CAMPUS').toUpperCase(),
    titleZh: value.titleZh ?? value.title_zh ?? value.title ?? '—',
    titleEn: value.titleEn ?? value.title_en ?? value.title ?? value.titleZh ?? '—',
    contentZh: value.contentZh ?? value.content_zh ?? value.content ?? value.body ?? '',
    contentEn: value.contentEn ?? value.content_en ?? value.content ?? value.body ?? value.contentZh ?? '',
    publishedAt: value.publishedAt ?? value.published_at ?? value.createdAt ?? value.created_at,
    published: value.published ?? value.isPublished ?? true
  });

  const normalizeSiteContent = (value = {}) => ({
    key: String(value.key ?? value.contentKey ?? value.content_key ?? '').trim(),
    zh: String(value.zh ?? value.zhText ?? value.zh_text ?? ''),
    en: String(value.en ?? value.enText ?? value.en_text ?? ''),
    updatedAt: value.updatedAt ?? value.updated_at ?? ''
  });

  const showToast = (message) => {
    const toast = document.querySelector('[data-toast]');
    if (!toast) return;
    toast.textContent = message;
    toast.classList.add('is-visible');
    window.clearTimeout(state.toastTimer);
    state.toastTimer = window.setTimeout(() => toast.classList.remove('is-visible'), 4200);
  };
  const showPageAlert = (message, kind = 'error') => {
    const alert = document.querySelector('[data-page-alert]');
    if (!alert) return;
    alert.textContent = message || '';
    alert.dataset.kind = kind;
  };

  const redirectToLogin = () => {
    clearSession();
    const target = `${window.location.pathname || '/portal'}${window.location.hash}`;
    window.location.href = `/login?next=${encodeURIComponent(target)}`;
  };
  const redirectAfterLogin = (user) => {
    const next = new URLSearchParams(window.location.search).get('next');
    if (next && /^\/(portal|admin)(?:#.*)?$/.test(next)) window.location.href = next;
    else window.location.href = user.role === 'admin' ? '/admin' : '/portal';
  };

  const setButtonLoading = (button, loading, labelKey) => {
    if (!button) return;
    button.disabled = loading;
    button.dataset.originalLabel ??= button.querySelector('[data-i18n]')?.textContent || button.textContent;
    const textNode = button.querySelector('[data-i18n]');
    if (textNode && labelKey) textNode.textContent = I18N.t(labelKey);
    button.classList.toggle('is-loading', loading);
  };

  const handleLogin = () => {
    const form = document.querySelector('[data-login-form]');
    if (!form) return;
    const alert = document.querySelector('[data-login-alert]');
    const submit = document.querySelector('[data-login-submit]');
    const setError = (key) => { if (alert) alert.textContent = I18N.t(key); };
    form.addEventListener('submit', async (event) => {
      event.preventDefault();
      if (!form.checkValidity()) { setError('login.errorRequired'); form.reportValidity(); return; }
      const values = Object.fromEntries(new FormData(form).entries());
      setError('');
      setButtonLoading(submit, true, 'login.loading');
      try {
        const result = await postJSON('/auth/login', 'POST', { username: values.username.trim(), password: values.password });
        const payload = result?.user ? result : (result?.account ? { user: result.account, ...result } : { user: result });
        const user = normalizeUser(payload.user || {});
        state.user = user;
        saveUser(user);
        redirectAfterLogin(user);
      } catch (error) {
        if (error.status === 401 || error.status === 403) setError('login.errorInvalid');
        else setError(error.network || error.status >= 500 ? 'login.errorUnavailable' : 'login.errorInvalid');
      } finally {
        setButtonLoading(submit, false, 'login.submit');
      }
    });
    const existing = readUser();
    if (existing) {
      apiAny(['/auth/me', '/me']).then((value) => { const user = normalizeUser(value); saveUser(user); redirectAfterLogin(user); }).catch((error) => {
        if (isAuthError(error)) clearSession();
      });
    }
  };

  const ensureSession = async (requiredRole = 'student') => {
    try {
      const value = await apiAny(['/auth/me', '/me']);
      state.user = normalizeUser(value);
      saveUser(state.user);
    } catch (error) {
      redirectToLogin();
      return false;
    }
    if (requiredRole === 'admin' && state.user.role !== 'admin') {
      showPageAlert(I18N.t('admin.accessDenied'));
      window.setTimeout(() => { window.location.href = '/portal'; }, 1000);
      return false;
    }
    if (requiredRole === 'student' && state.user.role === 'admin') {
      window.location.href = '/admin';
      return false;
    }
    document.querySelectorAll('[data-user-name]').forEach((node) => { node.textContent = state.user.name || state.user.username; });
    document.querySelectorAll('[data-user-welcome]').forEach((node) => { node.textContent = I18N.t('portal.welcome', { name: state.user.name || state.user.username }); });
    return true;
  };

  const setupCommon = () => {
    document.querySelectorAll('[data-logout]').forEach((button) => button.addEventListener('click', async () => {
      if (!window.confirm(I18N.t('portal.signOutConfirm'))) return;
      try { await apiAny(['/auth/logout', '/logout'], { method: 'POST', body: '{}' }); } catch { /* the local display state is still cleared */ }
      clearSession();
      window.location.href = '/login';
    }));
    document.querySelectorAll('[data-mobile-nav-toggle]').forEach((button) => {
      const menu = document.getElementById(button.getAttribute('aria-controls'));
      button.addEventListener('click', () => {
        const open = button.getAttribute('aria-expanded') !== 'true';
        button.setAttribute('aria-expanded', String(open));
        menu?.classList.toggle('is-open', open);
        menu?.setAttribute('aria-hidden', String(!open));
      });
      menu?.querySelectorAll('a').forEach((link) => link.addEventListener('click', () => { button.setAttribute('aria-expanded', 'false'); menu.classList.remove('is-open'); menu.setAttribute('aria-hidden', 'true'); }));
    });
  };

  const waitForManagedContent = async () => {
    try { await I18N.ready; I18N.apply(); } catch { /* static dictionary remains the fallback */ }
  };

  const setActiveNav = (selector, section) => document.querySelectorAll(selector).forEach((link) => link.classList.toggle('is-active', link.dataset.portalNav === section || link.dataset.adminNav === section));
  const setupHashNav = (kind) => {
    const selector = kind === 'admin' ? '[data-admin-nav]' : '[data-portal-nav]';
    const getSection = () => (window.location.hash || '#overview').slice(1).replace('admin-', '') || 'overview';
    const update = () => setActiveNav(selector, getSection());
    window.addEventListener('hashchange', update);
    update();
    document.querySelectorAll(selector).forEach((link) => link.addEventListener('click', () => window.setTimeout(update, 0)));
  };

  const resource = async (path, keys) => {
    try { return listFrom(await api(path), keys); }
    catch (error) {
      if (isAuthError(error)) redirectToLogin();
      throw error;
    }
  };

  const enrollmentIds = () => new Set(state.enrollments
    .filter((item) => String(item.status ?? item.enrollmentStatus ?? 'enrolled').toLowerCase() === 'enrolled')
    .map((item) => item.courseId ?? item.course_id ?? item.courseCode ?? item.code));
  const courseIsEnrolled = (course) => Boolean(course.enrolled) || enrollmentIds().has(course.id) || enrollmentIds().has(course.code);
  const courseLabel = (course) => localeValue({ zh: course.nameZh ?? course.courseNameZh, en: course.nameEn ?? course.courseNameEn }, course.code ?? course.courseCode);
  const courseSecondaryLabel = (course) => {
    const primary = courseLabel(course);
    const secondary = I18N.locale === 'zh' ? (course.nameEn ?? course.courseNameEn) : (course.nameZh ?? course.courseNameZh);
    return secondary && secondary !== primary ? `<small class="course-name">${escapeHTML(secondary)}</small>` : '';
  };
  const announcementTitle = (item) => localeValue({ zh: item.titleZh, en: item.titleEn }, '—');
  const announcementContent = (item) => localeValue({ zh: item.contentZh, en: item.contentEn }, '');
  const dayLabel = (day) => I18N.t(['', 'portal.mon', 'portal.tue', 'portal.wed', 'portal.thu', 'portal.fri', 'portal.sat', 'portal.sun'][day] || 'portal.mon');

  const renderUserFields = () => {
    if (!state.user) return;
    document.querySelectorAll('[data-user-name]').forEach((node) => { node.textContent = state.user.name || state.user.username; });
    document.querySelectorAll('[data-user-welcome]').forEach((node) => { node.textContent = I18N.t('portal.welcome', { name: state.user.name || state.user.username }); });
    const initials = String(state.user.name || state.user.username || 'CGU').trim().split(/\s+/).map((part) => part[0]).join('').slice(0, 3).toUpperCase();
    document.querySelectorAll('[data-user-initials]').forEach((node) => { node.textContent = initials || 'CGU'; });
    Object.entries({ studentId: state.user.studentId, username: state.user.username, email: state.user.email, college: localeValue(state.user.college, state.user.college), year: state.user.year }).forEach(([key, value]) => document.querySelectorAll(`[data-profile="${key}"]`).forEach((node) => { node.textContent = value || '—'; }));
  };

  const renderPortalMetrics = () => {
    const passed = state.grades.filter((grade) => grade.status !== 'inprogress' && grade.status !== 'in_progress' && grade.status !== 'inProgress' && grade.point !== '—');
    const credits = state.user?.credits ?? passed.reduce((sum, grade) => sum + numberValue(grade.credits, 0), 0);
    const points = passed.map((grade) => numberValue(grade.point, NaN)).filter(Number.isFinite);
    const gpa = state.user?.gpa ?? (points.length ? (points.reduce((sum, point) => sum + point, 0) / points.length).toFixed(2) : '—');
    const enrolled = state.courses.filter(courseIsEnrolled).length;
    const next = state.schedule[0];
    const metrics = { credits: credits || 0, gpa, enrolled, nextClass: next ? courseLabel(next) : I18N.t('portal.noNextClass') };
    const notes = { credits: I18N.t('portal.creditsTarget'), gpa: I18N.t('portal.gradedCourses', { count: passed.length }), enrolled: I18N.t('portal.currentTerm'), nextClass: next ? `${dayLabel(next.day)} · ${next.start}` : '—' };
    Object.entries(metrics).forEach(([key, value]) => document.querySelectorAll(`[data-metric="${key}"]`).forEach((node) => { node.textContent = value; }));
    Object.entries(notes).forEach(([key, value]) => document.querySelectorAll(`[data-metric-note="${key}"]`).forEach((node) => { node.textContent = value; }));
    document.querySelectorAll('[data-grade-summary]').forEach((node) => { node.textContent = `${passed.length}/${state.grades.length}`; });
    document.querySelectorAll('[data-course-count]').forEach((node) => { node.textContent = String(state.courses.length).padStart(2, '0'); });
    document.querySelectorAll('[data-term-label]').forEach((node) => { node.textContent = state.courses[0]?.term || I18N.t('portal.termFallback'); });
  };

  const renderCourseFilters = () => {
    const select = document.querySelector('[data-course-term]');
    if (!select) return;
    const selected = select.value || 'all';
    const terms = [...new Set(state.courses.map((course) => course.term).filter(Boolean))];
    select.innerHTML = `<option value="all">${escapeHTML(I18N.t('portal.allTerms'))}</option>${terms.map((term) => `<option value="${escapeHTML(term)}">${escapeHTML(term)}</option>`).join('')}`;
    select.value = terms.includes(selected) ? selected : 'all';
  };

  const renderCourses = () => {
    const list = document.querySelector('[data-course-list]');
    if (!list) return;
    renderCourseFilters();
    const query = String(document.querySelector('[data-course-search]')?.value || '').trim().toLowerCase();
    const term = document.querySelector('[data-course-term]')?.value || 'all';
    const type = document.querySelector('[data-course-type]')?.value || 'all';
    const visible = state.courses.filter((course) => {
      const haystack = `${course.code} ${course.nameZh} ${course.nameEn} ${course.teacher}`.toLowerCase();
      return (!query || haystack.includes(query)) && (term === 'all' || course.term === term) && (type === 'all' || course.type === type);
    });
    if (!visible.length) { list.innerHTML = `<tr><td colspan="7" class="empty-state">${escapeHTML(I18N.t('portal.noCourses'))}</td></tr>`; return; }
    list.innerHTML = visible.map((course) => {
      const enrolled = courseIsEnrolled(course);
      const full = course.capacity > 0 && course.enrolledCount >= course.capacity && !enrolled;
      const status = enrolled ? `<span class="status-pill">${escapeHTML(I18N.t('portal.enrolledLabel'))}</span>` : full ? `<span class="status-pill is-full">${escapeHTML(I18N.t('portal.full'))}</span>` : `<span class="status-pill">${escapeHTML(course.type === 'required' ? I18N.t('portal.required') : I18N.t('portal.elective'))}</span>`;
      const action = enrolled ? `<button class="table-action is-danger" type="button" data-course-action="drop" data-course-id="${escapeHTML(course.id)}">${escapeHTML(I18N.t('portal.drop'))}</button>` : `<button class="table-action" type="button" data-course-action="enroll" data-course-id="${escapeHTML(course.id)}"${full ? ' disabled' : ''}>${escapeHTML(I18N.t('portal.enroll'))}</button>`;
      return `<tr><td>${escapeHTML(course.code)}</td><td><span class="course-name">${escapeHTML(courseLabel(course))}</span><small class="course-name"><span>${escapeHTML(course.description || '')}</span></small></td><td>${escapeHTML(course.teacher)}</td><td>${escapeHTML(course.credits)}</td><td>${escapeHTML(course.term)}</td><td>${status}</td><td>${action}</td></tr>`;
    }).join('');
  };

  const renderGrades = () => {
    const list = document.querySelector('[data-grade-list]');
    if (!list) return;
    if (!state.grades.length) { list.innerHTML = `<tr><td colspan="5" class="empty-state">${escapeHTML(I18N.t('portal.noGrades'))}</td></tr>`; return; }
    list.innerHTML = state.grades.map((grade) => {
      const inProgress = ['inprogress', 'in_progress', 'inProgress'].includes(String(grade.status));
      const status = inProgress ? `<span class="status-pill is-progress">${escapeHTML(I18N.t('portal.inProgress'))}</span>` : `<span class="status-pill">${escapeHTML(I18N.t('portal.passed'))}</span>`;
      return `<tr><td><span class="course-name">${escapeHTML(localeValue({ zh: grade.courseNameZh, en: grade.courseNameEn }, grade.courseCode))}</span><small class="course-name">${escapeHTML(grade.courseCode)}</small></td><td>${escapeHTML(grade.score)}</td><td>${escapeHTML(grade.point)}</td><td>${escapeHTML(grade.term)}</td><td>${status}</td></tr>`;
    }).join('');
  };

  const renderSchedule = () => {
    const root = document.querySelector('[data-schedule-grid]');
    if (!root) return;
    if (!state.schedule.length) { root.innerHTML = `<p class="empty-state">${escapeHTML(I18N.t('portal.noSchedule'))}</p>`; return; }
    const slots = [...new Set(state.schedule.flatMap((entry) => [`${entry.start}|${entry.end}`]))].sort();
    const cells = [`<div class="schedule-cell schedule-head">${escapeHTML(I18N.t('portal.time'))}</div>`, ...[1, 2, 3, 4, 5, 6, 7].map((day) => `<div class="schedule-cell schedule-head">${escapeHTML(dayLabel(day))}</div>`), ...slots.flatMap((slot) => {
      const [start, end] = slot.split('|');
      return [`<div class="schedule-cell schedule-time">${escapeHTML(start)}<br>${escapeHTML(end)}</div>`, ...[1, 2, 3, 4, 5, 6, 7].map((day) => {
        const entry = state.schedule.find((item) => item.day === day && item.start === start && item.end === end);
        return `<div class="schedule-cell">${entry ? `<div class="schedule-entry"><strong>${escapeHTML(localeValue({ zh: entry.courseNameZh, en: entry.courseNameEn }, entry.courseCode))}</strong><span>${escapeHTML(entry.location || entry.courseCode)}</span></div>` : ''}</div>`;
      })];
    })];
    root.innerHTML = `<div class="schedule-grid">${cells.join('')}</div>`;
  };

  const renderAnnouncements = () => {
    const list = document.querySelector('[data-announcement-list]');
    if (list) {
      list.innerHTML = state.announcements.length ? state.announcements.map((item) => `<article class="announcement-item"><div class="announcement-date">${escapeHTML(shortDate(item.publishedAt))}<small>${escapeHTML(item.publishedAt ? new Date(item.publishedAt).getFullYear() : '')}</small></div><div><span class="announcement-type">${escapeHTML(item.type)}</span><h3>${escapeHTML(announcementTitle(item))}</h3><p>${escapeHTML(announcementContent(item))}</p></div><button class="announcement-open" type="button" data-open-announcement="${escapeHTML(item.id)}">${escapeHTML(I18N.t('portal.readMore'))} ↗</button></article>`).join('') : `<p class="empty-state">${escapeHTML(I18N.t('portal.noAnnouncements'))}</p>`;
    }
    const mini = document.querySelector('[data-mini-announcements]');
    if (mini) mini.innerHTML = state.announcements.slice(0, 3).map((item) => `<button class="mini-announcement" type="button" data-open-announcement="${escapeHTML(item.id)}"><time>${escapeHTML(shortDate(item.publishedAt))}</time><span><strong>${escapeHTML(announcementTitle(item))}</strong><span>${escapeHTML(item.type)}</span></span></button>`).join('') || `<p class="empty-state">${escapeHTML(I18N.t('portal.noAnnouncements'))}</p>`;
  };

  const renderMiniSchedule = () => {
    const mini = document.querySelector('[data-mini-schedule]');
    if (!mini) return;
    mini.innerHTML = state.schedule.slice(0, 3).map((entry) => `<div class="mini-class"><span><strong>${escapeHTML(localeValue({ zh: entry.courseNameZh, en: entry.courseNameEn }, entry.courseCode))}</strong><span>${escapeHTML(dayLabel(entry.day))} · ${escapeHTML(entry.location || entry.courseCode)}</span></span><time>${escapeHTML(entry.start)}</time></div>`).join('') || `<p class="empty-state">${escapeHTML(I18N.t('portal.noSchedule'))}</p>`;
  };

  const openAnnouncement = (id) => {
    const item = state.announcements.find((announcement) => String(announcement.id) === String(id));
    if (!item) return;
    const dialog = document.querySelector('[data-announcement-dialog]');
    if (!dialog) return;
    dialog.querySelector('[data-dialog-type]').textContent = item.type;
    dialog.querySelector('[data-dialog-title]').textContent = announcementTitle(item);
    dialog.querySelector('[data-dialog-date]').textContent = formatDate(item.publishedAt, true);
    dialog.querySelector('[data-dialog-body]').textContent = announcementContent(item);
    if (dialog.showModal) dialog.showModal(); else dialog.setAttribute('open', '');
  };

  const handleCourseAction = async (button) => {
    const id = button.dataset.courseId;
    const action = button.dataset.courseAction;
    const course = state.courses.find((item) => String(item.id) === String(id));
    if (!course) return;
    button.disabled = true;
    try {
      const response = await postJSON('/enrollments', 'POST', { courseId: id, course_id: id, action });
      course.enrolled = action === 'enroll';
      const activeRecord = state.enrollments.find((item) => String(item.courseId ?? item.course_id ?? item.courseCode ?? item.code) === String(id) && String(item.status ?? item.enrollmentStatus ?? 'enrolled').toLowerCase() === 'enrolled');
      if (action === 'enroll') {
        const returned = response?.enrollment || { courseId: id, status: 'enrolled' };
        state.enrollments = state.enrollments.filter((item) => String(item.courseId ?? item.course_id ?? item.courseCode ?? item.code) !== String(id) || String(item.status ?? item.enrollmentStatus ?? 'enrolled').toLowerCase() !== 'enrolled');
        state.enrollments.push(returned);
      } else if (activeRecord) {
        activeRecord.status = 'dropped';
      }
      renderPortal();
      showToast(I18N.t(action === 'enroll' ? 'portal.enrollSuccess' : 'portal.dropSuccess'));
    } catch (error) {
      if (isAuthError(error)) { redirectToLogin(); return; }
      showToast(error.message || I18N.t('portal.operationError'));
      button.disabled = false;
    }
  };

  const renderPortal = () => {
    renderUserFields();
    state.courses = state.courses.map(normalizeCourse);
    state.grades = state.grades.map(normalizeGrade);
    state.schedule = state.schedule.map(normalizeSchedule).sort((a, b) => a.day - b.day || a.start.localeCompare(b.start));
    state.announcements = state.announcements.map(normalizeAnnouncement);
    renderPortalMetrics(); renderCourses(); renderGrades(); renderSchedule(); renderAnnouncements(); renderMiniSchedule();
    I18N.apply();
    renderUserFields();
  };

  const bindPortalEvents = () => {
    const search = document.querySelector('[data-course-search]');
    const term = document.querySelector('[data-course-term]');
    const type = document.querySelector('[data-course-type]');
    [search, term, type].forEach((control) => control?.addEventListener('input', renderCourses));
    document.querySelector('[data-course-list]')?.addEventListener('click', (event) => { const button = event.target.closest('[data-course-action]'); if (button) handleCourseAction(button); });
    document.addEventListener('click', (event) => { const button = event.target.closest('[data-open-announcement]'); if (button) openAnnouncement(button.dataset.openAnnouncement); });
    document.querySelector('[data-close-dialog]')?.addEventListener('click', () => document.querySelector('[data-announcement-dialog]')?.close?.());
    document.querySelector('[data-announcement-dialog]')?.addEventListener('click', (event) => { if (event.target === event.currentTarget) event.currentTarget.close?.(); });
    document.querySelector('[data-refresh]')?.addEventListener('click', () => loadPortalData());
    window.addEventListener('hashchange', () => { const section = document.querySelector(window.location.hash || '#overview'); section?.scrollIntoView({ behavior: 'smooth' }); });
  };

  const loadPortalData = async () => {
    showPageAlert('');
    const refresh = document.querySelector('[data-refresh]');
    setButtonLoading(refresh, true);
    try {
      const [courses, enrollments, grades, schedule, announcements] = await Promise.all([
        resource('/courses', ['courses']),
        resource('/enrollments', ['enrollments', 'items']),
        resource('/grades', ['grades']),
        resource('/schedule', ['schedule', 'entries']),
        resource('/announcements', ['announcements', 'items'])
      ]);
      state.courses = courses.map(normalizeCourse);
      state.enrollments = enrollments;
      state.grades = grades.map(normalizeGrade);
      state.schedule = schedule.map(normalizeSchedule);
      state.announcements = announcements.map(normalizeAnnouncement);
      renderPortal();
    } catch (error) {
      if (!isAuthError(error)) showPageAlert(I18N.t('portal.loadError'));
    } finally { setButtonLoading(refresh, false, 'portal.refresh'); }
  };

  const initPortal = async () => {
    setupHashNav('portal'); bindPortalEvents();
    await waitForManagedContent();
    if (!await ensureSession('student')) return;
    await loadPortalData();
    window.addEventListener('cgu:localechange', renderPortal);
  };

  const adminPayload = (form) => {
    const values = Object.fromEntries(new FormData(form).entries());
    const nameZh = values.nameZh?.trim();
    const nameEn = values.nameEn?.trim() || nameZh;
    return {
      id: values.id?.trim() || undefined, code: values.code?.trim(), name: nameZh || nameEn, nameZh, nameEn, department: values.department?.trim() || '综合学院', term: values.term?.trim(), teacher: values.teacher?.trim(), credits: numberValue(values.credits, 0), capacity: numberValue(values.capacity, 0), type: values.type || 'elective', description: values.description?.trim() || ''
    };
  };
  const announcementPayload = (form) => {
    const values = Object.fromEntries(new FormData(form).entries());
    const titleZh = values.titleZh?.trim();
    const content = values.content?.trim();
    return { title: titleZh, titleZh, titleEn: values.titleEn?.trim() || titleZh, type: values.type?.trim() || 'CAMPUS', body: content, content, contentZh: content, contentEn: content, publishedAt: values.publishedAt ? new Date(values.publishedAt).toISOString() : new Date().toISOString(), published_at: values.publishedAt ? new Date(values.publishedAt).toISOString() : undefined, published: true, audience: 'all' };
  };

  const fillForm = (form, item, fields) => { fields.forEach((field) => { const input = form.elements[field]; if (input) input.value = item?.[field] ?? ''; }); };
  const showEditor = (editor, show) => { if (editor) editor.hidden = !show; if (show) editor?.querySelector('input:not([type="hidden"])')?.focus(); };

  const renderAdmin = () => {
    state.adminCourses = state.adminCourses.map(normalizeCourse);
    state.adminAnnouncements = state.adminAnnouncements.map(normalizeAnnouncement);
    const stats = state.adminStats?.stats || state.adminStats || {};
    document.querySelectorAll('[data-admin-metric="courses"]').forEach((node) => { node.textContent = stats.courses ?? state.adminCourses.length; });
    document.querySelectorAll('[data-admin-metric="sections"]').forEach((node) => { node.textContent = stats.sections ?? state.adminCourses.filter((course) => !(course.capacity && course.enrolledCount >= course.capacity)).length; });
    document.querySelectorAll('[data-admin-metric="students"]').forEach((node) => { node.textContent = stats.students ?? state.user?.studentCount ?? state.user?.stats?.students ?? '—'; });
    document.querySelectorAll('[data-admin-metric="pending"]').forEach((node) => { node.textContent = stats.pending ?? state.adminAnnouncements.filter((item) => !item.published).length; });
    const courseList = document.querySelector('[data-admin-course-list]');
    if (courseList) courseList.innerHTML = state.adminCourses.length ? state.adminCourses.map((course) => `<tr><td>${escapeHTML(course.code)}</td><td><span class="course-name">${escapeHTML(courseLabel(course))}</span>${courseSecondaryLabel(course)}</td><td>${escapeHTML(course.teacher)}</td><td>${escapeHTML(course.credits)}</td><td>${escapeHTML(course.term)}</td><td>${escapeHTML(course.enrolledCount)}/${escapeHTML(course.capacity || '—')}</td><td><button class="table-action" type="button" data-edit-course="${escapeHTML(course.id)}">${escapeHTML(I18N.t('admin.edit'))}</button><button class="table-action is-danger" type="button" data-delete-course="${escapeHTML(course.id)}">${escapeHTML(I18N.t('admin.delete'))}</button></td></tr>`).join('') : `<tr><td colspan="7" class="empty-state">${escapeHTML(I18N.t('admin.noCourses'))}</td></tr>`;
    const announcementList = document.querySelector('[data-admin-announcement-list]');
    if (announcementList) announcementList.innerHTML = state.adminAnnouncements.length ? state.adminAnnouncements.map((item) => `<article class="admin-announcement-row"><div><span class="announcement-type">${escapeHTML(item.type)}</span><h3>${escapeHTML(announcementTitle(item))}</h3><p>${escapeHTML(announcementContent(item))}</p></div><div class="admin-announcement-meta">${escapeHTML(formatDate(item.publishedAt, true))}</div><div class="admin-actions"><span class="status-pill${item.published ? '' : ' is-full'}">${escapeHTML(item.published ? I18N.t('admin.publish') : I18N.t('admin.unpublish'))}</span><button class="table-action" type="button" data-edit-announcement="${escapeHTML(item.id)}">${escapeHTML(I18N.t('admin.edit'))}</button><button class="table-action is-danger" type="button" data-delete-announcement="${escapeHTML(item.id)}">${escapeHTML(I18N.t('admin.delete'))}</button></div></article>`).join('') : `<p class="empty-state">${escapeHTML(I18N.t('admin.noAnnouncements'))}</p>`;
    renderSiteContent();
    I18N.apply();
  };

  const renderSiteContent = () => {
    const list = document.querySelector('[data-site-content-list]');
    if (!list) return;
    const query = String(document.querySelector('[data-site-content-search]')?.value || '').trim().toLowerCase();
    const visible = state.adminSiteContent.filter((item) => !query || `${item.key} ${item.zh} ${item.en}`.toLowerCase().includes(query));
    list.innerHTML = visible.length ? visible.map((item) => `<tr><td><code>${escapeHTML(item.key)}</code></td><td class="content-preview">${escapeHTML(item.zh)}</td><td class="content-preview">${escapeHTML(item.en)}</td><td><button class="table-action" type="button" data-edit-site-content="${escapeHTML(item.key)}">${escapeHTML(I18N.t('admin.edit'))}</button></td></tr>`).join('') : `<tr><td colspan="4" class="empty-state">${escapeHTML(I18N.t('admin.noSiteContent'))}</td></tr>`;
  };

  const adminRequest = async (path, method, body) => {
    return postJSON(path, method, body || {});
  };

  const handleAdminCourseForm = () => {
    const form = document.querySelector('[data-course-form]');
    const editor = document.querySelector('[data-course-editor]');
    if (!form) return;
    form.addEventListener('submit', async (event) => {
      event.preventDefault();
      if (!form.checkValidity()) { showToast(I18N.t('admin.required')); form.reportValidity(); return; }
      const values = adminPayload(form); const id = form.elements.id.value;
      try {
        if (id) {
          const result = await adminRequest(`/admin/courses/${encodeURIComponent(id)}`, 'PATCH', values);
          const updated = normalizeCourse(result?.course || result || { ...values, id });
          state.adminCourses = state.adminCourses.map((course) => String(course.id) === String(id) ? updated : course);
        } else {
          const result = await adminRequest('/admin/courses', 'POST', values);
          state.adminCourses.unshift(normalizeCourse(result?.course || result || { ...values, id: values.code }));
        }
        form.reset(); form.elements.id.value = ''; showEditor(editor, false); renderAdmin(); showToast(I18N.t('admin.saved'));
      } catch (error) { if (isAuthError(error)) redirectToLogin(); else showToast(error.message || I18N.t('admin.error')); }
    });
    document.querySelector('[data-new-course]')?.addEventListener('click', () => { form.reset(); form.elements.id.value = ''; showEditor(editor, true); });
    document.querySelector('[data-cancel-course]')?.addEventListener('click', () => { form.reset(); form.elements.id.value = ''; showEditor(editor, false); });
    document.querySelector('[data-admin-course-list]')?.addEventListener('click', async (event) => {
      const edit = event.target.closest('[data-edit-course]'); const remove = event.target.closest('[data-delete-course]');
      if (edit) { const item = state.adminCourses.find((course) => String(course.id) === String(edit.dataset.editCourse)); if (item) { fillForm(form, item, ['id', 'code', 'term', 'nameZh', 'nameEn', 'teacher', 'credits', 'capacity', 'type', 'description']); showEditor(editor, true); } }
      if (remove) {
        const item = state.adminCourses.find((course) => String(course.id) === String(remove.dataset.deleteCourse));
        if (!item || !window.confirm(I18N.t('admin.deleteConfirm'))) return;
        try { await adminRequest(`/admin/courses/${encodeURIComponent(item.id)}`, 'DELETE'); state.adminCourses = state.adminCourses.filter((course) => String(course.id) !== String(item.id)); renderAdmin(); showToast(I18N.t('admin.deleted')); } catch (error) { if (isAuthError(error)) redirectToLogin(); else showToast(error.message || I18N.t('admin.error')); }
      }
    });
  };

  const handleAdminAnnouncementForm = () => {
    const form = document.querySelector('[data-announcement-form]');
    const editor = document.querySelector('[data-announcement-editor]');
    if (!form) return;
    form.addEventListener('submit', async (event) => {
      event.preventDefault();
      if (!form.checkValidity()) { showToast(I18N.t('admin.required')); form.reportValidity(); return; }
      const values = announcementPayload(form); const id = form.elements.id.value;
      try {
        if (id) {
          const result = await adminRequest(`/admin/announcements/${encodeURIComponent(id)}`, 'PATCH', values);
          const updated = normalizeAnnouncement(result?.announcement || result || { ...values, id });
          state.adminAnnouncements = state.adminAnnouncements.map((item) => String(item.id) === String(id) ? updated : item);
        } else {
          const result = await adminRequest('/admin/announcements', 'POST', values);
          state.adminAnnouncements.unshift(normalizeAnnouncement(result?.announcement || result || { ...values, id: Date.now() }));
        }
        form.reset(); form.elements.id.value = ''; showEditor(editor, false); renderAdmin(); showToast(I18N.t('admin.saved'));
      } catch (error) { if (isAuthError(error)) redirectToLogin(); else showToast(error.message || I18N.t('admin.error')); }
    });
    document.querySelector('[data-new-announcement]')?.addEventListener('click', () => { form.reset(); form.elements.id.value = ''; showEditor(editor, true); });
    document.querySelector('[data-cancel-announcement]')?.addEventListener('click', () => { form.reset(); form.elements.id.value = ''; showEditor(editor, false); });
    document.querySelector('[data-admin-announcement-list]')?.addEventListener('click', async (event) => {
      const edit = event.target.closest('[data-edit-announcement]'); const remove = event.target.closest('[data-delete-announcement]');
      if (edit) { const item = state.adminAnnouncements.find((announcement) => String(announcement.id) === String(edit.dataset.editAnnouncement)); if (item) { fillForm(form, { ...item, content: announcementContent(item), publishedAt: item.publishedAt ? new Date(item.publishedAt).toISOString().slice(0, 16) : '' }, ['id', 'titleZh', 'titleEn', 'type', 'content', 'publishedAt']); showEditor(editor, true); } }
      if (remove) {
        const item = state.adminAnnouncements.find((announcement) => String(announcement.id) === String(remove.dataset.deleteAnnouncement));
        if (!item || !window.confirm(I18N.t('admin.deleteConfirm'))) return;
        try { await adminRequest(`/admin/announcements/${encodeURIComponent(item.id)}`, 'DELETE'); state.adminAnnouncements = state.adminAnnouncements.filter((announcement) => String(announcement.id) !== String(item.id)); renderAdmin(); showToast(I18N.t('admin.deleted')); } catch (error) { if (isAuthError(error)) redirectToLogin(); else showToast(error.message || I18N.t('admin.error')); }
      }
    });
  };

  const handleAdminSiteContent = () => {
    const form = document.querySelector('[data-site-content-form]');
    const editor = document.querySelector('[data-site-content-editor]');
    if (!form) return;
    const close = () => { form.reset(); form.elements.key.readOnly = false; showEditor(editor, false); };
    form.addEventListener('submit', async (event) => {
      event.preventDefault();
      if (!form.checkValidity()) { showToast(I18N.t('admin.required')); form.reportValidity(); return; }
      const values = Object.fromEntries(new FormData(form).entries());
      try {
        const result = await adminRequest('/admin/site-content', 'PUT', { key: values.key.trim(), zh: values.zh.trim(), en: values.en.trim() });
        const updated = normalizeSiteContent(result?.content || result);
        state.adminSiteContent = [...state.adminSiteContent.filter((item) => item.key !== updated.key), updated].sort((a, b) => a.key.localeCompare(b.key));
        I18N.mergeSiteContent?.([updated]);
        I18N.apply(); renderSiteContent(); close(); showToast(I18N.t('admin.saved'));
      } catch (error) { if (isAuthError(error)) redirectToLogin(); else showToast(error.message || I18N.t('admin.error')); }
    });
    document.querySelector('[data-new-site-content]')?.addEventListener('click', () => { form.reset(); form.elements.key.readOnly = false; showEditor(editor, true); });
    document.querySelector('[data-cancel-site-content]')?.addEventListener('click', close);
    document.querySelector('[data-site-content-search]')?.addEventListener('input', renderSiteContent);
    document.querySelector('[data-site-content-list]')?.addEventListener('click', (event) => {
      const edit = event.target.closest('[data-edit-site-content]');
      if (!edit) return;
      const item = state.adminSiteContent.find((value) => value.key === edit.dataset.editSiteContent);
      if (!item) return;
      fillForm(form, item, ['key', 'zh', 'en']);
      form.elements.key.readOnly = true;
      showEditor(editor, true);
    });
  };

  const loadAdminData = async () => {
    try {
      const [courses, announcements, stats] = await Promise.all([
        resource('/admin/courses', ['courses']),
        resource('/admin/announcements', ['announcements', 'items']),
        api('/admin/stats')
      ]);
      const content = await resource('/admin/site-content', ['content', 'items']);
      const managed = new Map((I18N.catalog?.() || []).map((item) => [item.key, normalizeSiteContent(item)]));
      content.map(normalizeSiteContent).forEach((item) => {
        const existing = managed.get(item.key) || {};
        managed.set(item.key, { ...existing, ...item, zh: item.zh || existing.zh || '', en: item.en || existing.en || '' });
      });
      state.adminCourses = courses.map(normalizeCourse); state.adminAnnouncements = announcements.map(normalizeAnnouncement); state.adminSiteContent = [...managed.values()].filter((item) => item.key).sort((a, b) => a.key.localeCompare(b.key)); state.adminStats = stats || {}; renderAdmin();
    } catch (error) { if (!isAuthError(error)) showPageAlert(I18N.t('admin.error')); }
  };

  const initAdmin = async () => {
    setupHashNav('admin');
    await waitForManagedContent();
    if (!await ensureSession('admin')) return;
    handleAdminCourseForm(); handleAdminAnnouncementForm(); handleAdminSiteContent();
    await loadAdminData();
    window.addEventListener('cgu:localechange', renderAdmin);
  };

  document.addEventListener('DOMContentLoaded', () => {
    setupCommon();
    if (page === 'login') handleLogin();
    if (page === 'portal') initPortal();
    if (page === 'admin') initAdmin();
  });
})();
