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
    mailbox: [],
    mailboxEmail: '',
    mailboxUnread: 0,
    portalLoading: false,
    adminCourses: [],
    adminAnnouncements: [],
    adminAdmissions: [],
    adminStudents: [],
    adminGrades: [],
    adminSchedule: [],
    adminMailbox: [],
    adminNotifications: [],
    adminNotificationUnread: 0,
    adminSiteContent: [],
    adminStats: {},
    adminLoading: false,
    // Ignore an older all-sections response that started before an editor
    // mutation completed. Without this guard a slow refresh could put a newly
    // approved admission back into the visible pending list.
    adminMutationVersion: 0,
    adminRefreshTimer: 0,
    toastTimer: 0
  };

  class ApiError extends Error {
    constructor(message, status = 0, code = '', payload = null) {
      super(message);
      this.name = 'ApiError';
      this.status = status;
      this.code = code;
      this.payload = payload;
      this.details = payload?.details ?? null;
      this.network = status === 0;
    }
  }

  const localizedError = (error, fallbackKey = 'common.error') => {
    if (error?.network) return I18N.t('common.networkError');
    if (Number(error?.status) >= 500 || error?.code === 'api_unavailable') return I18N.t('common.apiUnavailable');
    return I18N.t(fallbackKey);
  };

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

  // A stalled API request must not leave an admin table in its initial
  // "loading" state forever. The limit is longer than the SMTP relay budget
  // so a legitimate onboarding send is not abandoned by the browser first.
  const API_TIMEOUT_MS = 30000;
  const fetchAPI = async (url, request) => {
    if (!window.AbortController) return fetch(url, request);
    const controller = new AbortController();
    const timer = window.setTimeout(() => controller.abort(), API_TIMEOUT_MS);
    let detachCaller = () => {};
    if (request.signal) {
      if (request.signal.aborted) controller.abort();
      else {
        const abortCaller = () => controller.abort();
        request.signal.addEventListener('abort', abortCaller, { once: true });
        detachCaller = () => request.signal.removeEventListener('abort', abortCaller);
      }
    }
    try {
      return await fetch(url, { ...request, signal: controller.signal });
    } finally {
      window.clearTimeout(timer);
      detachCaller();
    }
  };

  const api = async (path, options = {}) => {
    const request = { credentials: 'include', ...options, headers: { ...jsonHeaders(), ...(options.headers || {}) } };
    let response;
    try {
      response = await fetchAPI(`${API_BASE}${path}`, request);
    } catch (error) {
      throw new ApiError(I18N.t('common.networkError'));
    }
    let payload = null;
    const text = await response.text();
    if (text) {
      try { payload = JSON.parse(text); } catch { payload = { data: text }; }
    }
    const errorValue = payload?.error;
    const errorMessage = typeof errorValue === 'string' ? (payload?.message || errorValue) : errorValue?.message;
    if (!response.ok || payload?.ok === false) {
      const errorCode = typeof errorValue === 'string' ? errorValue : errorValue?.code || '';
      throw new ApiError(errorMessage || response.statusText || I18N.t('common.error'), response.status, errorCode, payload);
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
    throw lastError || new ApiError(I18N.t('common.apiUnavailable'), 404);
  };

  const postJSON = (path, method, body) => api(path, { method, body: JSON.stringify(body) });
  const isAuthError = (error) => error?.status === 401 || error?.status === 403;

  const escapeHTML = (value) => String(value ?? '').replace(/[&<>"']/g, (character) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[character]));
  const localeValue = (value, fallback = '') => I18N.pick(value, fallback);
  const translated = (item, base, fallback = '') => localeValue(item?.[base] || item?.[`${base}Zh`] && { zh: item[`${base}Zh`], en: item[`${base}En`] }, fallback);
  const numberValue = (value, fallback = 0) => Number.isFinite(Number(value)) ? Number(value) : fallback;
  const formatDate = (value, withTime = false) => {
    if (!value) return I18N.t('common.notAvailable');
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return escapeHTML(value);
    return new Intl.DateTimeFormat(I18N.locale === 'zh' ? 'zh-CN' : 'en-US', withTime ? { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' } : { year: 'numeric', month: '2-digit', day: '2-digit' }).format(date);
  };
  const shortDate = (value) => {
    if (!value) return I18N.t('common.notAvailable');
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return I18N.t('common.notAvailable');
    return new Intl.DateTimeFormat(I18N.locale === 'zh' ? 'zh-CN' : 'en-US', { month: '2-digit', day: '2-digit' }).format(date);
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
      name: user.name ?? user.displayName ?? user.realName ?? user.username ?? user.account ?? '',
      role: role === 'administrator' || role === 'staff' ? 'admin' : role,
      studentId: user.studentId ?? user.student_id ?? user.id ?? '',
      email: user.email ?? '',
      studentEmail: user.studentEmail ?? user.student_email ?? '',
      college: user.college ?? user.school ?? user.department ?? '',
      year: user.year ?? user.grade ?? user.enrollmentYear ?? ''
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
      teacher: value.teacher ?? value.instructor ?? value.teacherName ?? '',
      credits: numberValue(value.credits ?? value.credit, 0),
      term: value.term ?? value.semester ?? value.period ?? '',
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
    courseCode: value.courseCode ?? value.course_code ?? value.code ?? '',
    courseNameZh: value.courseNameZh ?? value.course_name_zh ?? value.courseName ?? value.course_name ?? value.name ?? '',
    courseNameEn: value.courseNameEn ?? value.course_name_en ?? value.courseName ?? value.course_name ?? value.name ?? '',
    score: value.score ?? value.mark ?? value.result ?? '',
    point: value.point ?? value.gpa ?? value.gradePoint ?? '',
    term: value.term ?? value.semester ?? '',
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
      courseNameZh: value.courseNameZh ?? value.course_name_zh ?? value.courseName ?? value.course_name ?? value.name ?? '',
      courseNameEn: value.courseNameEn ?? value.course_name_en ?? value.courseName ?? value.course_name ?? value.name ?? '',
      location: value.location ?? value.room ?? value.classroom ?? ''
    };
  };

  const normalizeAnnouncement = (value = {}) => ({
    ...value,
    id: value.id ?? value.announcementId ?? value.announcement_id ?? `${value.title || value.titleZh || Math.random()}`,
    type: String(value.type ?? value.category ?? 'CAMPUS').toUpperCase(),
    titleZh: value.titleZh ?? value.title_zh ?? value.title ?? '',
    titleEn: value.titleEn ?? value.title_en ?? value.title ?? value.titleZh ?? '',
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

  const normalizeAdmission = (value = {}) => ({
    ...value,
    id: value.id ?? value.applicationId ?? value.application_id ?? '',
    name: String(value.name ?? value.applicantName ?? '').trim(),
    email: String(value.email ?? '').trim(),
    school: String(value.school ?? value.program ?? '').trim(),
    status: String(value.status ?? 'pending').toLowerCase(),
    notes: String(value.notes ?? '').trim(),
    createdAt: value.createdAt ?? value.created_at ?? value.submittedAt ?? '',
    updatedAt: value.updatedAt ?? value.updated_at ?? '',
    studentId: String(value.studentId ?? value.student_id ?? '').trim(),
    approvedAt: value.approvedAt ?? value.approved_at ?? '',
    issuedAt: value.issuedAt ?? value.issued_at ?? '',
    deliveryStatus: String(value.deliveryStatus ?? value.delivery_status ?? '').trim(),
    deliveryError: String(value.deliveryError ?? value.delivery_error ?? '').trim()
  });

  const normalizeAdminStudent = (value = {}) => ({
    ...value,
    id: value.id ?? value.userId ?? value.username ?? '',
    username: value.username ?? value.account ?? '',
    name: value.name ?? value.displayName ?? value.username ?? '',
    email: value.email ?? '',
    studentEmail: value.studentEmail ?? value.student_email ?? '',
    studentId: value.studentId ?? value.student_id ?? '',
    college: value.college ?? value.department ?? '',
    year: value.year ?? value.grade ?? '',
    role: String(value.role ?? 'student').toLowerCase(),
    admissionApproved: Boolean(value.admissionApproved ?? value.admission_approved),
    active: value.active === undefined && value.disabled === undefined ? true : Boolean(value.active ?? !value.disabled)
  });

  const normalizeAdminGrade = (value = {}) => ({
    ...normalizeGrade(value),
    studentId: value.studentId ?? value.student_id ?? value.userId ?? '',
    credits: numberValue(value.credits ?? value.credit, 0),
    status: String(value.status ?? 'inprogress').toLowerCase()
  });

  const normalizeAdminSchedule = (value = {}) => ({
    ...normalizeSchedule(value),
    studentId: value.studentId ?? value.student_id ?? value.userId ?? '',
    teacher: value.teacher ?? value.instructor ?? '',
    location: value.location ?? value.room ?? value.classroom ?? ''
  });

  const normalizeMailbox = (value = {}) => ({
    ...value,
    id: value.id ?? value.messageId ?? value.message_id ?? '',
    senderName: value.senderName ?? value.sender_name ?? value.sender ?? '',
    subject: String(value.subject ?? value.title ?? '').trim(),
    body: String(value.body ?? value.message ?? value.content ?? '').trim(),
    createdAt: value.createdAt ?? value.created_at ?? '',
    readAt: value.readAt ?? value.read_at ?? '',
    read: Boolean(value.read ?? value.isRead ?? value.readAt ?? value.read_at)
  });

  const normalizeAdminMailbox = (value = {}) => ({
    ...normalizeMailbox(value),
    recipientId: value.recipientId ?? value.recipient_id ?? '',
    recipientName: value.recipientName ?? value.recipient_name ?? '',
    recipientStudentId: value.recipientStudentId ?? value.recipient_student_id ?? '',
    recipientEmail: value.recipientEmail ?? value.recipient_email ?? '',
    deliveryMode: value.deliveryMode ?? value.delivery_mode ?? 'internal',
    externalRecipient: value.externalRecipient ?? value.external_recipient ?? '',
    deliveryStatus: value.deliveryStatus ?? value.delivery_status ?? 'internal',
    deliveryError: value.deliveryError ?? value.delivery_error ?? '',
    deliveredAt: value.deliveredAt ?? value.delivered_at ?? ''
  });

  const normalizeAdminNotification = (value = {}) => ({
    ...value,
    id: String(value.id ?? value.notificationId ?? value.notification_id ?? '').trim(),
    type: String(value.type ?? value.typeName ?? value.type_name ?? 'NOTICE').toUpperCase(),
    titleZh: String(value.titleZh ?? value.title_zh ?? value.title ?? '').trim(),
    titleEn: String(value.titleEn ?? value.title_en ?? value.title ?? '').trim(),
    bodyZh: String(value.bodyZh ?? value.body_zh ?? value.body ?? '').trim(),
    bodyEn: String(value.bodyEn ?? value.body_en ?? value.body ?? '').trim(),
    referenceId: String(value.referenceId ?? value.reference_id ?? '').trim(),
    createdAt: value.createdAt ?? value.created_at ?? '',
    readAt: value.readAt ?? value.read_at ?? ''
  });

  const mailboxDeliveryLabel = (status) => I18N.t(({ internal: 'admin.deliveryInternal', pending: 'admin.deliveryPending', sending: 'admin.deliverySending', sent: 'admin.deliverySent', failed: 'admin.deliveryFailed', not_configured: 'admin.deliveryNotConfigured', unknown: 'admin.deliveryUnknown' }[status] || 'admin.deliveryInternal'));
  const mailboxDeliveryClass = (status) => status === 'failed' || status === 'not_configured' || status === 'unknown' ? ' is-full' : status === 'pending' || status === 'sending' ? ' is-progress' : '';

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

  const newRequestKey = () => {
    if (window.crypto?.randomUUID) return window.crypto.randomUUID();
    const bytes = new Uint8Array(16);
    if (window.crypto?.getRandomValues) {
      window.crypto.getRandomValues(bytes);
      return Array.from(bytes, (value) => value.toString(16).padStart(2, '0')).join('');
    }
    return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}-${Math.random().toString(36).slice(2)}`;
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
        if (error.code === 'origin_not_allowed' || error.code === 'csrf_required') setError('login.errorServiceConfig');
        else if (error.status === 401) setError('login.errorInvalid');
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
    const displayName = state.user.name || state.user.username || I18N.t('brand.short');
    document.querySelectorAll('[data-user-name]').forEach((node) => { node.textContent = displayName; });
    document.querySelectorAll('[data-user-welcome]').forEach((node) => { node.textContent = I18N.t('portal.welcome', { name: displayName }); });
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
    try {
      // A stalled content service must never prevent page controls from binding.
      // The catalog is assembled from the managed-content endpoint and the
      // homepage markup sequentially. Give both bounded requests time to
      // finish so the first admin view contains the complete editable catalog.
      await Promise.race([Promise.resolve(I18N.ready), new Promise((resolve) => window.setTimeout(resolve, 6500))]);
      I18N.apply();
    } catch { /* static dictionary remains the fallback */ }
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
  const optionalResource = async (path, keys) => {
    try { return await resource(path, keys); }
    catch (error) {
      if (error?.status === 404 || error?.status === 405) return [];
      throw error;
    }
  };
  const protectedResponse = async (path) => {
    try { return await api(path); }
    catch (error) {
      if (isAuthError(error)) redirectToLogin();
      throw error;
    }
  };

  const enrollmentIds = () => new Set(state.enrollments
    .filter((item) => String(item.status ?? item.enrollmentStatus ?? 'enrolled').toLowerCase() === 'enrolled')
    .map((item) => item.courseId ?? item.course_id ?? item.courseCode ?? item.code));
  const courseIsEnrolled = (course) => Boolean(course.enrolled) || enrollmentIds().has(course.id) || enrollmentIds().has(course.code);
  const courseLabel = (course) => localeValue({ zh: course.nameZh ?? course.courseNameZh, en: course.nameEn ?? course.courseNameEn }, course.code ?? course.courseCode) || notAvailable();
  const courseSecondaryLabel = (course) => {
    const primary = courseLabel(course);
    const secondary = I18N.locale === 'zh' ? (course.nameEn ?? course.courseNameEn) : (course.nameZh ?? course.courseNameZh);
    return secondary && secondary !== primary ? `<small class="course-name">${escapeHTML(secondary)}</small>` : '';
  };
  const notAvailable = () => I18N.t('common.notAvailable');
  const announcementType = (value) => {
    const key = String(value || 'NOTICE').trim().toUpperCase().replace(/[- ]/g, '_');
    return I18N.t(({ ADMISSIONS: 'common.typeAdmissions', ACADEMICS: 'common.typeAcademics', CAMPUS: 'common.typeCampus', RESEARCH: 'common.typeResearch', WORLD_UPDATE: 'common.typeWorldUpdate', NOTICE: 'common.typeNotice' }[key] || 'common.typeNotice'));
  };
  const announcementTitle = (item) => localeValue({ zh: item.titleZh, en: item.titleEn }, notAvailable());
  const announcementContent = (item) => localeValue({ zh: item.contentZh, en: item.contentEn }, '');
  const dayLabel = (day) => I18N.t(['', 'portal.mon', 'portal.tue', 'portal.wed', 'portal.thu', 'portal.fri', 'portal.sat', 'portal.sun'][day] || 'portal.mon');

  const renderUserFields = () => {
    if (!state.user) return;
    const displayName = state.user.name || state.user.username || I18N.t('brand.short');
    document.querySelectorAll('[data-user-name]').forEach((node) => { node.textContent = displayName; });
    document.querySelectorAll('[data-user-welcome]').forEach((node) => { node.textContent = I18N.t('portal.welcome', { name: displayName }); });
    const initials = String(state.user.name || state.user.username || I18N.t('brand.short')).trim().split(/\s+/).map((part) => part[0]).join('').slice(0, 3).toUpperCase();
    document.querySelectorAll('[data-user-initials]').forEach((node) => { node.textContent = initials || I18N.t('brand.short'); });
    Object.entries({ studentId: state.user.studentId, username: state.user.username, email: state.user.email, studentEmail: state.user.studentEmail, college: localeValue(state.user.college, state.user.college), year: state.user.year }).forEach(([key, value]) => document.querySelectorAll(`[data-profile="${key}"]`).forEach((node) => { node.textContent = value || notAvailable(); }));
  };

  const renderPortalMetrics = () => {
    const passed = state.grades.filter((grade) => grade.status !== 'inprogress' && grade.status !== 'in_progress' && grade.status !== 'inProgress' && grade.point !== '');
    const credits = state.user?.credits ?? passed.reduce((sum, grade) => sum + numberValue(grade.credits, 0), 0);
    const points = passed.map((grade) => numberValue(grade.point, NaN)).filter(Number.isFinite);
    const gpa = state.user?.gpa ?? (points.length ? (points.reduce((sum, point) => sum + point, 0) / points.length).toFixed(2) : notAvailable());
    const enrolled = state.courses.filter(courseIsEnrolled).length;
    const next = state.schedule[0];
    const metrics = { credits: credits || 0, gpa, enrolled, nextClass: next ? courseLabel(next) : I18N.t('portal.noNextClass') };
    const notes = { credits: I18N.t('portal.creditsTarget'), gpa: I18N.t('portal.gradedCourses', { count: passed.length }), enrolled: I18N.t('portal.currentTerm'), nextClass: next ? `${dayLabel(next.day)} · ${next.start}` : notAvailable() };
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
      return `<tr><td>${escapeHTML(course.code || notAvailable())}</td><td><span class="course-name">${escapeHTML(courseLabel(course))}</span><small class="course-name"><span>${escapeHTML(course.description || '')}</span></small></td><td>${escapeHTML(course.teacher || notAvailable())}</td><td>${escapeHTML(course.credits ?? notAvailable())}</td><td>${escapeHTML(course.term || notAvailable())}</td><td>${status}</td><td>${action}</td></tr>`;
    }).join('');
  };

  const renderGrades = () => {
    const list = document.querySelector('[data-grade-list]');
    if (!list) return;
    if (!state.grades.length) { list.innerHTML = `<tr><td colspan="5" class="empty-state">${escapeHTML(I18N.t('portal.noGrades'))}</td></tr>`; return; }
    list.innerHTML = state.grades.map((grade) => {
      const inProgress = ['inprogress', 'in_progress', 'inProgress'].includes(String(grade.status));
      const status = inProgress ? `<span class="status-pill is-progress">${escapeHTML(I18N.t('portal.inProgress'))}</span>` : `<span class="status-pill">${escapeHTML(I18N.t('portal.passed'))}</span>`;
      const gradeName = localeValue({ zh: grade.courseNameZh, en: grade.courseNameEn }, grade.courseCode) || notAvailable();
      const score = grade.score === '' || grade.score == null ? notAvailable() : grade.score;
      const point = grade.point === '' || grade.point == null ? notAvailable() : grade.point;
      return `<tr><td><span class="course-name">${escapeHTML(gradeName)}</span><small class="course-name">${escapeHTML(grade.courseCode || notAvailable())}</small></td><td>${escapeHTML(score)}</td><td>${escapeHTML(point)}</td><td>${escapeHTML(grade.term || notAvailable())}</td><td>${status}</td></tr>`;
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
        return `<div class="schedule-cell">${entry ? `<div class="schedule-entry"><strong>${escapeHTML(localeValue({ zh: entry.courseNameZh, en: entry.courseNameEn }, entry.courseCode) || notAvailable())}</strong><span>${escapeHTML(entry.location || entry.courseCode || notAvailable())}</span></div>` : ''}</div>`;
      })];
    })];
    root.innerHTML = `<div class="schedule-grid">${cells.join('')}</div>`;
  };

  const renderAnnouncements = () => {
    const list = document.querySelector('[data-announcement-list]');
    if (list) {
      list.innerHTML = state.announcements.length ? state.announcements.map((item) => `<article class="announcement-item"><div class="announcement-date">${escapeHTML(shortDate(item.publishedAt))}<small>${escapeHTML(item.publishedAt ? new Date(item.publishedAt).getFullYear() : '')}</small></div><div><span class="announcement-type">${escapeHTML(announcementType(item.type))}</span><h3>${escapeHTML(announcementTitle(item))}</h3><p>${escapeHTML(announcementContent(item))}</p></div><button class="announcement-open" type="button" data-open-announcement="${escapeHTML(item.id)}">${escapeHTML(I18N.t('portal.readMore'))} ↗</button></article>`).join('') : `<p class="empty-state">${escapeHTML(I18N.t('portal.noAnnouncements'))}</p>`;
    }
    const mini = document.querySelector('[data-mini-announcements]');
    if (mini) mini.innerHTML = state.announcements.slice(0, 3).map((item) => `<button class="mini-announcement" type="button" data-open-announcement="${escapeHTML(item.id)}"><time>${escapeHTML(shortDate(item.publishedAt))}</time><span><strong>${escapeHTML(announcementTitle(item))}</strong><span>${escapeHTML(announcementType(item.type))}</span></span></button>`).join('') || `<p class="empty-state">${escapeHTML(I18N.t('portal.noAnnouncements'))}</p>`;
  };

  const renderMiniSchedule = () => {
    const mini = document.querySelector('[data-mini-schedule]');
    if (!mini) return;
    mini.innerHTML = state.schedule.slice(0, 3).map((entry) => `<div class="mini-class"><span><strong>${escapeHTML(localeValue({ zh: entry.courseNameZh, en: entry.courseNameEn }, entry.courseCode) || notAvailable())}</strong><span>${escapeHTML(dayLabel(entry.day))} · ${escapeHTML(entry.location || entry.courseCode || notAvailable())}</span></span><time>${escapeHTML(entry.start || notAvailable())}</time></div>`).join('') || `<p class="empty-state">${escapeHTML(I18N.t('portal.noSchedule'))}</p>`;
  };

  const renderMailbox = () => {
    const address = document.querySelector('[data-mailbox-address]');
    if (address) address.textContent = state.mailboxEmail || state.user?.studentEmail || notAvailable();
    document.querySelectorAll('[data-mailbox-unread]').forEach((node) => { node.textContent = String(state.mailboxUnread); });
    const unreadLabel = document.querySelector('[data-mailbox-unread-label]');
    if (unreadLabel) unreadLabel.textContent = state.mailboxUnread ? I18N.t('portal.unreadCount', { count: state.mailboxUnread }) : I18N.t('portal.allRead');
    const list = document.querySelector('[data-mailbox-list]');
    if (!list) return;
    if (!state.mailbox.length) {
      list.innerHTML = `<p class="empty-state">${escapeHTML(I18N.t('portal.noMailboxMessages'))}</p>`;
      return;
    }
    list.innerHTML = state.mailbox.map((item) => `<article class="mailbox-item${item.read ? '' : ' is-unread'}"><div class="mailbox-item-heading"><div><span class="announcement-type">${escapeHTML(I18N.t('portal.sender'))}</span><h3>${escapeHTML(item.subject || notAvailable())}</h3></div><time>${escapeHTML(formatDate(item.createdAt, true))}</time></div><p class="mailbox-sender">${escapeHTML(item.senderName || I18N.t('brand.short'))}</p><p class="mailbox-body">${escapeHTML(item.body).replace(/\n/g, '<br>')}</p><div class="mailbox-item-actions">${item.read ? `<span class="status-pill">${escapeHTML(I18N.t('portal.read'))}</span>` : `<button class="table-action" type="button" data-mailbox-read="${escapeHTML(item.id)}">${escapeHTML(I18N.t('portal.markRead'))}</button>`}</div></article>`).join('');
  };

  const openAnnouncement = (id) => {
    const item = state.announcements.find((announcement) => String(announcement.id) === String(id));
    if (!item) return;
    const dialog = document.querySelector('[data-announcement-dialog]');
    if (!dialog) return;
    dialog.querySelector('[data-dialog-type]').textContent = announcementType(item.type);
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
      showToast(localizedError(error, 'portal.operationError'));
      button.disabled = false;
    }
  };

  const renderPortal = () => {
    renderUserFields();
    state.courses = state.courses.map(normalizeCourse);
    state.grades = state.grades.map(normalizeGrade);
    state.schedule = state.schedule.map(normalizeSchedule).sort((a, b) => a.day - b.day || a.start.localeCompare(b.start));
    state.announcements = state.announcements.map(normalizeAnnouncement);
    renderPortalMetrics(); renderCourses(); renderGrades(); renderSchedule(); renderAnnouncements(); renderMiniSchedule(); renderMailbox();
    I18N.apply();
    renderUserFields();
  };

  const handlePasswordChange = () => {
    const form = document.querySelector('[data-password-form]');
    if (!form) return;
    const alert = form.querySelector('[data-password-alert]');
    const submit = form.querySelector('[data-password-submit]');
    form.addEventListener('submit', async (event) => {
      event.preventDefault();
      if (!form.checkValidity()) {
        if (alert) alert.textContent = I18N.t('portal.passwordRequired');
        form.reportValidity();
        return;
      }
      const values = Object.fromEntries(new FormData(form).entries());
      if (values.newPassword !== values.confirmPassword) {
        if (alert) alert.textContent = I18N.t('portal.passwordMismatch');
        form.elements.confirmPassword.focus();
        return;
      }
      if (alert) alert.textContent = '';
      setButtonLoading(submit, true, 'portal.savingPassword');
      try {
        const result = await postJSON('/auth/password', 'POST', {
          currentPassword: values.currentPassword,
          newPassword: values.newPassword,
          confirmPassword: values.confirmPassword
        });
        state.user = normalizeUser(result?.user || result);
        saveUser(state.user);
        form.reset();
        if (alert) alert.textContent = '';
        showToast(I18N.t('portal.passwordChanged'));
      } catch (error) {
        if (error.code === 'current_password_invalid') {
          if (alert) alert.textContent = I18N.t('portal.passwordInvalid');
        } else if (error.code === 'password_unchanged') {
          if (alert) alert.textContent = I18N.t('portal.passwordUnchanged');
        } else if (isAuthError(error)) {
          redirectToLogin();
        } else if (alert) {
          alert.textContent = error.code === 'invalid_input' ? I18N.t('portal.passwordRequired') : I18N.t('portal.passwordSaveError');
        }
      } finally {
        setButtonLoading(submit, false, 'portal.savePassword');
      }
    });
  };

  const bindPortalEvents = () => {
    // Bind local controls before any remote content/session request. A slow or
    // unavailable API must not make the portal appear non-interactive.
    handlePasswordChange();
    const search = document.querySelector('[data-course-search]');
    const term = document.querySelector('[data-course-term]');
    const type = document.querySelector('[data-course-type]');
    [search, term, type].forEach((control) => control?.addEventListener('input', renderCourses));
    document.querySelector('[data-course-list]')?.addEventListener('click', (event) => { const button = event.target.closest('[data-course-action]'); if (button) handleCourseAction(button); });
    document.querySelector('[data-mailbox-list]')?.addEventListener('click', async (event) => {
      const button = event.target.closest('[data-mailbox-read]');
      if (!button) return;
      button.disabled = true;
      try {
        await postJSON(`/mailbox/${encodeURIComponent(button.dataset.mailboxRead)}`, 'PATCH', { read: true });
        state.mailbox = state.mailbox.map((item) => String(item.id) === String(button.dataset.mailboxRead) ? { ...item, read: true, readAt: new Date().toISOString() } : item);
        state.mailboxUnread = state.mailbox.filter((item) => !item.read).length;
        renderMailbox();
        showToast(I18N.t('portal.mailboxMarkedRead'));
      } catch (error) {
        if (isAuthError(error)) redirectToLogin();
        else { button.disabled = false; showToast(localizedError(error, 'portal.operationError')); }
      }
    });
    document.addEventListener('click', (event) => { const button = event.target.closest('[data-open-announcement]'); if (button) openAnnouncement(button.dataset.openAnnouncement); });
    document.querySelector('[data-close-dialog]')?.addEventListener('click', () => document.querySelector('[data-announcement-dialog]')?.close?.());
    document.querySelector('[data-announcement-dialog]')?.addEventListener('click', (event) => { if (event.target === event.currentTarget) event.currentTarget.close?.(); });
    document.querySelector('[data-refresh]')?.addEventListener('click', () => loadPortalData());
    window.addEventListener('hashchange', () => { const section = document.querySelector(window.location.hash || '#overview'); section?.scrollIntoView({ behavior: 'smooth' }); });
  };

  const loadPortalData = async () => {
    if (state.portalLoading) return;
    state.portalLoading = true;
    showPageAlert('');
    const refresh = document.querySelector('[data-refresh]');
    setButtonLoading(refresh, true, 'portal.refreshing');
    try {
      const results = await Promise.allSettled([
        resource('/courses', ['courses']),
        resource('/enrollments', ['enrollments', 'items']),
        resource('/grades', ['grades']),
        resource('/schedule', ['schedule', 'entries']),
        resource('/announcements', ['announcements', 'items']),
        protectedResponse('/mailbox')
      ]);
      const authFailure = results.find((result) => result.status === 'rejected' && isAuthError(result.reason));
      if (authFailure) {
        redirectToLogin();
        return;
      }
      const valueAt = (index, fallback) => results[index]?.status === 'fulfilled' ? results[index].value : fallback;
      const courses = valueAt(0, state.courses);
      const enrollments = valueAt(1, state.enrollments);
      const grades = valueAt(2, state.grades);
      const schedule = valueAt(3, state.schedule);
      const announcements = valueAt(4, state.announcements);
      const mailbox = valueAt(5, { messages: state.mailbox, email: state.mailboxEmail, unread: state.mailboxUnread });
      state.courses = courses.map(normalizeCourse);
      state.enrollments = enrollments;
      state.grades = grades.map(normalizeGrade);
      state.schedule = schedule.map(normalizeSchedule);
      state.announcements = announcements.map(normalizeAnnouncement);
      state.mailbox = listFrom(mailbox, ['messages', 'items']).map(normalizeMailbox);
      state.mailboxEmail = mailbox?.email || state.user?.studentEmail || '';
      state.mailboxUnread = Number.isFinite(Number(mailbox?.unread)) ? Number(mailbox.unread) : state.mailbox.filter((item) => !item.read).length;
      renderPortal();
      if (results.some((result) => result.status === 'rejected')) showPageAlert(I18N.t('portal.loadPartialError'));
    } catch (error) {
      if (!isAuthError(error)) showPageAlert(I18N.t('portal.loadError'));
    } finally {
      state.portalLoading = false;
      setButtonLoading(refresh, false, 'portal.refresh');
    }
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
    const id = values.id?.trim() || '';
    const nameZh = values.nameZh?.trim() || '';
    const nameEn = values.nameEn?.trim() || '';
    const clearFields = id ? ['nameEn', 'department', 'teacher', 'description', 'term', 'type'].filter((field) => !String(values[field] || '').trim()) : [];
    return {
      id: id || undefined, code: values.code?.trim(), name: nameZh || nameEn, nameZh, nameEn, department: values.department?.trim() || '', term: values.term?.trim() || '', teacher: values.teacher?.trim() || '', credits: numberValue(values.credits, 0), capacity: numberValue(values.capacity, 0), type: values.type?.trim() || '', description: values.description?.trim() || '', clearFields
    };
  };
  const announcementPayload = (form) => {
    const values = Object.fromEntries(new FormData(form).entries());
    const id = values.id?.trim() || '';
    const titleZh = values.titleZh?.trim() || '';
    const titleEn = values.titleEn?.trim() || '';
    const content = values.content?.trim() || '';
    const contentEn = values.contentEn?.trim() || '';
    const publishedAt = values.publishedAt?.trim() || '';
    const clearFields = id ? ['titleEn', 'contentEn', 'type', 'publishedAt'].filter((field) => !String(values[field] || '').trim()) : [];
    return { id: id || undefined, title: titleZh, titleZh, titleEn, type: values.type?.trim() || '', body: content, content, contentZh: content, contentEn, publishedAt: publishedAt ? new Date(publishedAt).toISOString() : '', published_at: publishedAt ? new Date(publishedAt).toISOString() : '', published: form.elements.published ? form.elements.published.checked : true, audience: 'all', clearFields };
  };

  const fillForm = (form, item, fields) => { fields.forEach((field) => { const input = form.elements[field]; if (!input) return; if (input.type === 'checkbox') input.checked = Boolean(item?.[field]); else input.value = item?.[field] ?? ''; }); };
  const showEditor = (editor, show) => { if (editor) editor.hidden = !show; if (show) editor?.querySelector('input:not([type="hidden"])')?.focus(); };

  const adminStudent = (id) => state.adminStudents.find((item) => String(item.id) === String(id));
  const adminCourse = (id) => state.adminCourses.find((item) => String(item.id) === String(id));
  const adminStudentLabel = (item) => item ? `${item.studentId || item.username || notAvailable()} · ${item.name || item.username || notAvailable()}` : notAvailable();
  const adminCourseLabel = (item) => item ? `${item.code || item.id || notAvailable()} · ${courseLabel(item)}` : notAvailable();
  const adminDayLabel = (day) => I18N.t(['portal.mon', 'portal.tue', 'portal.wed', 'portal.thu', 'portal.fri', 'portal.sat', 'portal.sun'][Math.max(1, Math.min(7, Number(day) || 1)) - 1]);

  const renderAdminReferenceOptions = () => {
    const studentOptions = state.adminStudents.filter((item) => item.active).map((item) => `<option value="${escapeHTML(item.id)}">${escapeHTML(adminStudentLabel(item))}</option>`).join('');
    const courseOptions = state.adminCourses.map((item) => `<option value="${escapeHTML(item.id)}">${escapeHTML(adminCourseLabel(item))}</option>`).join('');
    document.querySelectorAll('[data-admin-student-options]').forEach((select) => {
      const current = select.value;
      select.innerHTML = `<option value="">${escapeHTML(I18N.t('admin.selectStudent'))}</option>${studentOptions}`;
      if (state.adminStudents.some((item) => String(item.id) === String(current))) select.value = current;
    });
    document.querySelectorAll('[data-admin-course-options]').forEach((select) => {
      const current = select.value;
      select.innerHTML = `<option value="">${escapeHTML(I18N.t('admin.selectCourse'))}</option>${courseOptions}`;
      if (state.adminCourses.some((item) => String(item.id) === String(current))) select.value = current;
    });
    document.querySelectorAll('[data-admin-mailbox-student-options]').forEach((select) => {
      const current = select.value;
      select.innerHTML = `<option value="">${escapeHTML(I18N.t('admin.selectStudent'))}</option>${studentOptions}`;
      if (state.adminStudents.some((item) => String(item.id) === String(current))) select.value = current;
    });
  };

  const renderAdminStudents = () => {
    const list = document.querySelector('[data-admin-student-list]');
    if (!list) return;
     list.innerHTML = state.adminStudents.length ? state.adminStudents.map((item) => `<tr class="${item.active ? '' : 'is-disabled'}"><td>${escapeHTML(item.studentId || notAvailable())}</td><td><span class="course-name">${escapeHTML(item.name || notAvailable())}</span></td><td>${escapeHTML(item.username || notAvailable())}</td><td>${escapeHTML(item.studentEmail || item.email || notAvailable())}</td><td>${escapeHTML(item.college || notAvailable())}</td><td>${escapeHTML(item.year || notAvailable())}</td><td><span class="status-pill${item.active ? '' : ' is-full'}">${escapeHTML(item.active ? I18N.t('admin.studentActive') : I18N.t('admin.studentDisabled'))}</span><button class="table-action" type="button" data-edit-student="${escapeHTML(item.id)}">${escapeHTML(I18N.t('admin.edit'))}</button><button class="table-action${item.active ? ' is-danger' : ''}" type="button" data-toggle-student="${escapeHTML(item.id)}">${escapeHTML(I18N.t(item.active ? 'admin.studentDisable' : 'admin.studentEnable'))}</button></td></tr>`).join('') : `<tr><td colspan="7" class="empty-state">${escapeHTML(I18N.t('admin.noStudents'))}</td></tr>`;
  };

  const renderAdminAcademic = () => {
    const gradeList = document.querySelector('[data-admin-grade-list]');
    if (gradeList) gradeList.innerHTML = state.adminGrades.length ? state.adminGrades.map((item) => {
      const student = adminStudent(item.studentId);
      const course = adminCourse(item.courseId);
      const statusKey = { inprogress: 'admin.gradeInProgress', graded: 'admin.gradeGraded', published: 'admin.gradePublished', withdrawn: 'admin.gradeWithdrawn' }[item.status] || 'admin.gradeInProgress';
       return `<tr><td>${escapeHTML(student ? adminStudentLabel(student) : (item.studentId || notAvailable()))}</td><td><span class="course-name">${escapeHTML(course ? adminCourseLabel(course) : (item.courseCode || item.courseNameZh || notAvailable()))}</span></td><td>${escapeHTML(item.score ?? notAvailable())}</td><td>${escapeHTML(item.point ?? notAvailable())}</td><td><span class="status-pill${item.status === 'withdrawn' ? ' is-full' : item.status === 'inprogress' ? ' is-progress' : ''}">${escapeHTML(I18N.t(statusKey))}</span></td><td><button class="table-action" type="button" data-edit-grade="${escapeHTML(item.id)}">${escapeHTML(I18N.t('admin.edit'))}</button><button class="table-action is-danger" type="button" data-delete-grade="${escapeHTML(item.id)}">${escapeHTML(I18N.t('admin.delete'))}</button></td></tr>`;
    }).join('') : `<tr><td colspan="6" class="empty-state">${escapeHTML(I18N.t('admin.noGrades'))}</td></tr>`;
    const scheduleList = document.querySelector('[data-admin-schedule-list]');
    if (scheduleList) scheduleList.innerHTML = state.adminSchedule.length ? state.adminSchedule.map((item) => {
      const student = adminStudent(item.studentId);
      const course = adminCourse(item.courseId);
       return `<tr><td>${escapeHTML(student ? adminStudentLabel(student) : (item.studentId || notAvailable()))}</td><td><span class="course-name">${escapeHTML(course ? adminCourseLabel(course) : (item.courseCode || item.courseNameZh || notAvailable()))}</span></td><td>${escapeHTML(adminDayLabel(item.day))}</td><td>${escapeHTML(`${item.start || notAvailable()}–${item.end || notAvailable()}`)}</td><td>${escapeHTML(item.location || notAvailable())}</td><td><button class="table-action" type="button" data-edit-schedule="${escapeHTML(item.id)}">${escapeHTML(I18N.t('admin.edit'))}</button><button class="table-action is-danger" type="button" data-delete-schedule="${escapeHTML(item.id)}">${escapeHTML(I18N.t('admin.delete'))}</button></td></tr>`;
    }).join('') : `<tr><td colspan="6" class="empty-state">${escapeHTML(I18N.t('admin.noSchedule'))}</td></tr>`;
  };

  const renderAdmin = () => {
    state.adminCourses = state.adminCourses.map(normalizeCourse);
    state.adminAnnouncements = state.adminAnnouncements.map(normalizeAnnouncement);
    state.adminAdmissions = state.adminAdmissions.map(normalizeAdmission);
    state.adminStudents = state.adminStudents.map(normalizeAdminStudent);
    state.adminGrades = state.adminGrades.map(normalizeAdminGrade);
    state.adminSchedule = state.adminSchedule.map(normalizeAdminSchedule).sort((a, b) => a.day - b.day || a.start.localeCompare(b.start));
    state.adminNotifications = state.adminNotifications.map(normalizeAdminNotification);
    const stats = state.adminStats?.stats || state.adminStats || {};
    document.querySelectorAll('[data-admin-metric="courses"]').forEach((node) => { node.textContent = stats.courses ?? state.adminCourses.length; });
    document.querySelectorAll('[data-admin-metric="sections"]').forEach((node) => { node.textContent = stats.sections ?? state.adminCourses.filter((course) => !(course.capacity && course.enrolledCount >= course.capacity)).length; });
    document.querySelectorAll('[data-admin-metric="students"]').forEach((node) => { node.textContent = stats.students ?? state.user?.studentCount ?? state.user?.stats?.students ?? notAvailable(); });
    document.querySelectorAll('[data-admin-metric="pending"]').forEach((node) => { node.textContent = stats.pending ?? state.adminAnnouncements.filter((item) => !item.published).length; });
    const courseList = document.querySelector('[data-admin-course-list]');
    if (courseList) courseList.innerHTML = state.adminCourses.length ? state.adminCourses.map((course) => `<tr><td>${escapeHTML(course.code || notAvailable())}</td><td><span class="course-name">${escapeHTML(courseLabel(course) || notAvailable())}</span>${courseSecondaryLabel(course)}</td><td>${escapeHTML(course.department || notAvailable())}</td><td>${escapeHTML(course.teacher || notAvailable())}</td><td>${escapeHTML(course.credits ?? notAvailable())}</td><td>${escapeHTML(course.term || notAvailable())}</td><td>${escapeHTML(course.enrolledCount)}/${escapeHTML(course.capacity || notAvailable())}</td><td><button class="table-action" type="button" data-edit-course="${escapeHTML(course.id)}">${escapeHTML(I18N.t('admin.edit'))}</button><button class="table-action is-danger" type="button" data-delete-course="${escapeHTML(course.id)}">${escapeHTML(I18N.t('admin.delete'))}</button></td></tr>`).join('') : `<tr><td colspan="8" class="empty-state">${escapeHTML(I18N.t('admin.noCourses'))}</td></tr>`;
    const announcementList = document.querySelector('[data-admin-announcement-list]');
    if (announcementList) announcementList.innerHTML = state.adminAnnouncements.length ? state.adminAnnouncements.map((item) => `<article class="admin-announcement-row"><div><span class="announcement-type">${escapeHTML(announcementType(item.type))}</span><h3>${escapeHTML(announcementTitle(item))}</h3><p>${escapeHTML(announcementContent(item))}</p></div><div class="admin-announcement-meta">${escapeHTML(formatDate(item.publishedAt, true))}</div><div class="admin-actions"><span class="status-pill${item.published ? '' : ' is-full'}">${escapeHTML(item.published ? I18N.t('admin.publish') : I18N.t('admin.unpublish'))}</span><button class="table-action" type="button" data-edit-announcement="${escapeHTML(item.id)}">${escapeHTML(I18N.t('admin.edit'))}</button><button class="table-action is-danger" type="button" data-delete-announcement="${escapeHTML(item.id)}">${escapeHTML(I18N.t('admin.delete'))}</button></div></article>`).join('') : `<p class="empty-state">${escapeHTML(I18N.t('admin.noAnnouncements'))}</p>`;
    renderAdminAdmissions();
    renderAdminNotifications();
    renderAdminStudents();
    renderAdminReferenceOptions();
    renderAdminAcademic();
    renderAdminMailbox();
    renderSiteContent();
    I18N.apply();
  };

  const renderAdminAdmissions = () => {
    const list = document.querySelector('[data-admin-admission-list]');
    if (!list) return;
    list.innerHTML = state.adminAdmissions.length ? state.adminAdmissions.map((item) => {
      const status = String(item.status || 'pending').toLowerCase();
      const approved = status === 'accepted' && Boolean(item.studentId);
      const terminal = status === 'rejected' || status === 'withdrawn';
      const incomplete = status === 'accepted' && !item.studentId;
      const credentials = item.issuedCredentials || item.credentials;
      const credentialPanel = credentials?.password ? `<div class="admission-credentials" role="status"><strong>${escapeHTML(I18N.t('admin.admissionCredentials'))}</strong><span>${escapeHTML(I18N.t('admin.admissionUsername'))}: ${escapeHTML(credentials.username || notAvailable())}</span><span>${escapeHTML(I18N.t('admin.admissionPassword'))}: <code>${escapeHTML(credentials.password)}</code></span><span>${escapeHTML(I18N.t('portal.studentEmail'))}: ${escapeHTML(credentials.studentEmail || credentials.email || notAvailable())}</span><button class="table-action" type="button" data-admission-copy-password="${escapeHTML(credentials.password)}">${escapeHTML(I18N.t('admin.admissionCopyPassword'))}</button></div>` : '';
      const deliveryStatus = item.deliveryStatus || '';
      const deliveryNotice = deliveryStatus === 'sent' ? I18N.t('admin.admissionEmailDelivery') : deliveryStatus === 'not_configured' ? I18N.t('admin.admissionEmailPending') : deliveryStatus === 'failed' ? I18N.t('admin.smtpFailed') : deliveryStatus === 'unknown' ? I18N.t('admin.smtpUnknown') : deliveryStatus ? I18N.t('admin.admissionEmailPending') : '';
      const deliveryDiagnostic = (deliveryStatus === 'failed' || deliveryStatus === 'unknown') && item.deliveryError ? ` ${escapeHTML(item.deliveryError)}` : '';
      const deliveryPanel = deliveryNotice ? `<small class="admission-provisioned">${escapeHTML(deliveryNotice)}${deliveryDiagnostic}</small>` : '';
      const notesPanel = `<div class="admission-notes-editor" data-admission-notes-editor><label><span>${escapeHTML(I18N.t('admin.notes'))}</span><textarea data-admission-notes rows="2" maxlength="2000" placeholder="${escapeHTML(I18N.t('admin.notesPlaceholder'))}">${escapeHTML(item.notes || '')}</textarea></label><button class="table-action" type="button" data-admission-save-notes><span data-i18n="admin.saveNotes">${escapeHTML(I18N.t('admin.saveNotes'))}</span></button></div>`;
      const statusKey = incomplete ? 'admin.admissionIncomplete' : status === 'accepted' ? 'admin.admissionAccepted' : status === 'reviewing' ? 'admin.admissionReviewing' : status === 'contacted' ? 'admin.admissionContacted' : 'admin.admissionPending';
      const action = approved ? `<div class="admin-actions"><span class="status-pill">${escapeHTML(I18N.t('admin.admissionApproved'))}</span><small class="admin-announcement-meta">${escapeHTML(item.studentId || notAvailable())}</small></div>` : terminal ? `<div class="admin-actions"><span class="status-pill is-full">${escapeHTML(I18N.t(status === 'withdrawn' ? 'admin.admissionWithdrawn' : 'admin.admissionRejected'))}</span></div>` : `<div class="admin-actions"><span class="status-pill is-progress">${escapeHTML(I18N.t(statusKey))}</span><button class="portal-button portal-button-gold" type="button" data-admission-approve="${escapeHTML(item.id)}"><span>${escapeHTML(I18N.t(incomplete ? 'admin.admissionRepair' : 'admin.admissionApprove'))}</span><span aria-hidden="true">→</span></button></div>`;
      return `<article class="admin-admission-row${approved ? ' is-approved' : terminal || incomplete ? ' is-closed' : ''}" data-admission-id="${escapeHTML(item.id)}"><div><span class="announcement-type">${escapeHTML(item.school || I18N.t('admin.admissionUndecided'))}</span><h3>${escapeHTML(item.name || notAvailable())}</h3><p><a href="mailto:${escapeHTML(item.email)}">${escapeHTML(item.email || notAvailable())}</a></p>${approved ? `<small class="admission-provisioned">${escapeHTML(I18N.t('admin.admissionProvisioned'))}</small>${deliveryPanel}` : ''}${credentialPanel}${notesPanel}</div><div class="admin-announcement-meta">${escapeHTML(formatDate(item.createdAt, true))}</div>${action}</article>`;
    }).join('') : `<p class="empty-state">${escapeHTML(I18N.t('admin.noAdmissions'))}</p>`;
  };

  const renderAdminNotifications = () => {
    const unread = Number(state.adminNotificationUnread) || state.adminNotifications.filter((item) => !item.readAt).length;
    document.querySelectorAll('[data-admin-notification-unread]').forEach((node) => { node.textContent = String(unread); });
    document.querySelectorAll('[data-admin-notification-count]').forEach((node) => {
      node.textContent = String(unread);
      node.hidden = unread === 0;
    });
    const list = document.querySelector('[data-admin-notification-list]');
    if (!list) return;
    list.innerHTML = state.adminNotifications.length ? state.adminNotifications.map((item) => {
      const title = localeValue({ zh: item.titleZh, en: item.titleEn }, I18N.t('admin.notificationAdmissions'));
      const body = localeValue({ zh: item.bodyZh, en: item.bodyEn }, '');
      const link = item.referenceId ? `<a class="table-action" href="#admin-admissions" data-i18n="admin.notificationOpenAdmissions">${escapeHTML(I18N.t('admin.notificationOpenAdmissions'))}</a>` : '';
      const mark = item.readAt ? `<span class="status-pill">${escapeHTML(I18N.t('admin.read'))}</span>` : `<button class="table-action" type="button" data-admin-notification-read="${escapeHTML(item.id)}">${escapeHTML(I18N.t('admin.notificationMarkRead'))}</button>`;
      return `<article class="admin-notification-row${item.readAt ? '' : ' is-unread'}"><div><span class="announcement-type">${escapeHTML(announcementType(item.type))}</span><h3>${escapeHTML(title)}</h3><p>${escapeHTML(body)}</p></div><time>${escapeHTML(formatDate(item.createdAt, true))}</time><div class="admin-actions">${link}${mark}</div></article>`;
    }).join('') : `<p class="empty-state">${escapeHTML(I18N.t('admin.noNotifications'))}</p>`;
  };

  const renderAdminMailbox = () => {
    const list = document.querySelector('[data-admin-mailbox-list]');
    document.querySelectorAll('[data-admin-mailbox-count]').forEach((node) => { node.textContent = String(state.adminMailbox.length); });
    if (!list) return;
    list.innerHTML = state.adminMailbox.length ? state.adminMailbox.map((item) => {
      const status = item.deliveryStatus || 'internal';
      const retryable = status === 'failed' || status === 'not_configured' || status === 'unknown';
      const deliveryTarget = item.externalRecipient ? `<span>${escapeHTML(I18N.t('admin.deliveryTarget'))}: ${escapeHTML(item.externalRecipient)}</span>` : '';
      const deliveryError = item.deliveryError ? `<small class="mailbox-delivery-error">${escapeHTML(item.deliveryError)}</small>` : '';
      const retry = retryable ? `<button class="table-action" type="button" data-retry-mailbox="${escapeHTML(item.id)}" data-retry-confirm="${status === 'unknown' ? 'true' : 'false'}">${escapeHTML(I18N.t('admin.retryDelivery'))}</button>` : '';
      return `<article class="mailbox-item admin-mailbox-item${item.read ? '' : ' is-unread'}"><div class="mailbox-item-heading"><div><span class="announcement-type">${escapeHTML(item.recipientStudentId || item.recipientEmail || notAvailable())}</span><h3>${escapeHTML(item.subject || notAvailable())}</h3></div><time>${escapeHTML(formatDate(item.createdAt, true))}</time></div><p class="mailbox-sender">${escapeHTML(item.recipientName || notAvailable())} · ${escapeHTML(item.recipientEmail || notAvailable())}</p><p class="mailbox-body">${escapeHTML(item.body).replace(/\n/g, '<br>')}</p><div class="mailbox-delivery-meta"><span class="status-pill${mailboxDeliveryClass(status)}">${escapeHTML(mailboxDeliveryLabel(status))}</span>${deliveryTarget}</div>${deliveryError}<div class="mailbox-item-actions"><span class="status-pill${item.read ? '' : ' is-progress'}">${escapeHTML(item.read ? I18N.t('admin.read') : I18N.t('admin.unread'))}</span>${retry}</div></article>`;
    }).join('') : `<p class="empty-state">${escapeHTML(I18N.t('admin.noMailboxMessages'))}</p>`;
  };

  const renderAdminLoadError = () => {
    const message = I18N.t('admin.loadError');
    document.querySelectorAll('[data-admin-course-list]').forEach((list) => { list.innerHTML = `<tr><td colspan="8" class="empty-state">${escapeHTML(message)}</td></tr>`; });
    document.querySelectorAll('[data-admin-announcement-list], [data-admin-admission-list], [data-admin-mailbox-list]').forEach((list) => { list.innerHTML = `<p class="empty-state">${escapeHTML(message)}</p>`; });
    document.querySelectorAll('[data-admin-student-list], [data-admin-grade-list], [data-admin-schedule-list]').forEach((list) => { list.innerHTML = `<tr><td colspan="7" class="empty-state">${escapeHTML(message)}</td></tr>`; });
    document.querySelectorAll('[data-site-content-list]').forEach((list) => { list.innerHTML = `<tr><td colspan="4" class="empty-state">${escapeHTML(message)}</td></tr>`; });
  };

  const renderSiteContent = () => {
    const list = document.querySelector('[data-site-content-list]');
    if (!list) return;
    const query = String(document.querySelector('[data-site-content-search]')?.value || '').trim().toLowerCase();
    const visible = state.adminSiteContent.filter((item) => !query || `${item.key} ${item.zh} ${item.en}`.toLowerCase().includes(query));
    list.innerHTML = visible.length ? visible.map((item) => `<tr><td><code>${escapeHTML(item.key)}</code></td><td class="content-preview">${escapeHTML(item.zh)}</td><td class="content-preview">${escapeHTML(item.en)}</td><td><button class="table-action" type="button" data-edit-site-content="${escapeHTML(item.key)}">${escapeHTML(I18N.t('admin.edit'))}</button></td></tr>`).join('') : `<tr><td colspan="4" class="empty-state">${escapeHTML(I18N.t('admin.noSiteContent'))}</td></tr>`;
  };

  const adminRequest = async (path, method, body) => {
    state.adminMutationVersion += 1;
    return postJSON(path, method, body || {});
  };

  // Every admin editor shares the same submit lock. This gives an immediate
  // visual response and prevents a double click from creating duplicate
  // records while the request is in flight.
  const beginAdminFormSubmit = (form) => {
    if (!form || form.dataset.submitting === 'true') return false;
    form.dataset.submitting = 'true';
    setButtonLoading(form.querySelector('button[type="submit"]'), true, 'admin.saving');
    return true;
  };
  const endAdminFormSubmit = (form) => {
    if (!form) return;
    delete form.dataset.submitting;
    setButtonLoading(form.querySelector('button[type="submit"]'), false, 'admin.save');
  };

  const handleAdminCourseForm = () => {
    const form = document.querySelector('[data-course-form]');
    const editor = document.querySelector('[data-course-editor]');
    if (!form) return;
    form.addEventListener('submit', async (event) => {
      event.preventDefault();
      if (!form.checkValidity()) { showToast(I18N.t('admin.required')); form.reportValidity(); return; }
      if (!beginAdminFormSubmit(form)) return;
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
      } catch (error) { if (isAuthError(error)) redirectToLogin(); else showToast(localizedError(error, 'admin.error')); }
      finally { endAdminFormSubmit(form); }
    });
    document.querySelector('[data-new-course]')?.addEventListener('click', () => { form.reset(); form.elements.id.value = ''; showEditor(editor, true); });
    document.querySelector('[data-cancel-course]')?.addEventListener('click', () => { form.reset(); form.elements.id.value = ''; showEditor(editor, false); });
    document.querySelector('[data-admin-course-list]')?.addEventListener('click', async (event) => {
      const edit = event.target.closest('[data-edit-course]'); const remove = event.target.closest('[data-delete-course]');
      if (edit) { const item = state.adminCourses.find((course) => String(course.id) === String(edit.dataset.editCourse)); if (item) { fillForm(form, item, ['id', 'code', 'term', 'nameZh', 'nameEn', 'department', 'teacher', 'credits', 'capacity', 'type', 'description']); showEditor(editor, true); } }
      if (remove) {
        const item = state.adminCourses.find((course) => String(course.id) === String(remove.dataset.deleteCourse));
        if (!item || !window.confirm(I18N.t('admin.deleteConfirm'))) return;
        try { await adminRequest(`/admin/courses/${encodeURIComponent(item.id)}`, 'DELETE'); state.adminCourses = state.adminCourses.filter((course) => String(course.id) !== String(item.id)); renderAdmin(); showToast(I18N.t('admin.deleted')); } catch (error) { if (isAuthError(error)) redirectToLogin(); else showToast(localizedError(error, 'admin.error')); }
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
      if (!beginAdminFormSubmit(form)) return;
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
      } catch (error) { if (isAuthError(error)) redirectToLogin(); else showToast(localizedError(error, 'admin.error')); }
      finally { endAdminFormSubmit(form); }
    });
    document.querySelector('[data-new-announcement]')?.addEventListener('click', () => { form.reset(); form.elements.id.value = ''; showEditor(editor, true); });
    document.querySelector('[data-cancel-announcement]')?.addEventListener('click', () => { form.reset(); form.elements.id.value = ''; showEditor(editor, false); });
    document.querySelector('[data-admin-announcement-list]')?.addEventListener('click', async (event) => {
      const edit = event.target.closest('[data-edit-announcement]'); const remove = event.target.closest('[data-delete-announcement]');
      if (edit) { const item = state.adminAnnouncements.find((announcement) => String(announcement.id) === String(edit.dataset.editAnnouncement)); if (item) { fillForm(form, { ...item, content: item.contentZh, contentEn: item.contentEn, publishedAt: item.publishedAt ? new Date(item.publishedAt).toISOString().slice(0, 16) : '' }, ['id', 'titleZh', 'titleEn', 'type', 'content', 'contentEn', 'publishedAt', 'published']); showEditor(editor, true); } }
      if (remove) {
        const item = state.adminAnnouncements.find((announcement) => String(announcement.id) === String(remove.dataset.deleteAnnouncement));
        if (!item || !window.confirm(I18N.t('admin.deleteConfirm'))) return;
        try { await adminRequest(`/admin/announcements/${encodeURIComponent(item.id)}`, 'DELETE'); state.adminAnnouncements = state.adminAnnouncements.filter((announcement) => String(announcement.id) !== String(item.id)); renderAdmin(); showToast(I18N.t('admin.deleted')); } catch (error) { if (isAuthError(error)) redirectToLogin(); else showToast(localizedError(error, 'admin.error')); }
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
      if (!beginAdminFormSubmit(form)) return;
      const values = Object.fromEntries(new FormData(form).entries());
      try {
        const result = await adminRequest('/admin/site-content', 'PUT', { key: values.key.trim(), zh: values.zh.trim(), en: values.en.trim() });
        const updated = normalizeSiteContent(result?.content || result);
        state.adminSiteContent = [...state.adminSiteContent.filter((item) => item.key !== updated.key), updated].sort((a, b) => a.key.localeCompare(b.key));
        I18N.mergeSiteContent?.([updated]);
        I18N.apply(); renderSiteContent(); close(); showToast(I18N.t('admin.saved'));
      } catch (error) { if (isAuthError(error)) redirectToLogin(); else showToast(localizedError(error, 'admin.error')); }
      finally { endAdminFormSubmit(form); }
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

  const handleAdminAdmissions = () => {
    const list = document.querySelector('[data-admin-admission-list]');
    if (!list) return;
    list.addEventListener('click', async (event) => {
      const copyButton = event.target.closest('[data-admission-copy-password]');
      if (copyButton) {
        const password = copyButton.dataset.admissionCopyPassword || '';
        try {
          if (navigator.clipboard?.writeText) {
            await navigator.clipboard.writeText(password);
          } else {
            const input = document.createElement('textarea');
            input.value = password;
            input.setAttribute('readonly', '');
            input.style.position = 'fixed';
            input.style.opacity = '0';
            document.body.appendChild(input);
            input.select();
            const copied = document.execCommand('copy');
            input.remove();
            if (!copied) throw new Error('clipboard unavailable');
          }
          showToast(I18N.t('admin.admissionCopied'));
        } catch { showToast(I18N.t('admin.admissionCopyFailed')); }
        return;
      }
      const notesButton = event.target.closest('[data-admission-save-notes]');
      if (notesButton) {
        const row = notesButton.closest('[data-admission-id]');
        const item = state.adminAdmissions.find((value) => String(value.id) === String(row?.dataset.admissionId));
        const notes = row?.querySelector('[data-admission-notes]')?.value?.trim() || '';
        if (!item || !row) return;
        setButtonLoading(notesButton, true, 'admin.savingNotes');
        try {
          const result = await adminRequest(`/admin/admissions/${encodeURIComponent(item.id)}`, 'PATCH', { notes, clearNotes: notes === '' });
          const updated = normalizeAdmission(result?.application || result || item);
          updated.issuedCredentials = item.issuedCredentials;
          state.adminAdmissions = state.adminAdmissions.map((value) => String(value.id) === String(item.id) ? updated : value);
          renderAdminAdmissions();
          showToast(I18N.t('admin.notesSaved'));
        } catch (error) {
          if (isAuthError(error)) redirectToLogin(); else showToast(localizedError(error, 'admin.error'));
        } finally {
          setButtonLoading(notesButton, false, 'admin.saveNotes');
        }
        return;
      }
      const button = event.target.closest('[data-admission-approve]');
      if (!button) return;
      const item = state.adminAdmissions.find((value) => String(value.id) === String(button.dataset.admissionApprove));
      if (!item || item.status === 'rejected' || item.status === 'withdrawn' || (item.status === 'accepted' && item.studentId)) return;
      setButtonLoading(button, true, 'admin.approving');
      try {
        const result = await adminRequest(`/admin/admissions/${encodeURIComponent(item.id)}/approve`, 'POST', {});
        const updated = normalizeAdmission(result?.application || result || item);
        updated.deliveryStatus = String(result?.deliveryStatus || updated.deliveryStatus || '').trim();
        updated.deliveryError = String(result?.deliveryError || updated.deliveryError || '').trim();
        const initialPassword = String(result?.initialPassword || '').trim();
        updated.issuedCredentials = result?.credentials || (initialPassword ? {
          username: result?.student?.username || '',
          password: initialPassword,
          studentEmail: result?.student?.studentEmail || ''
        } : null);
        state.adminAdmissions = state.adminAdmissions.map((value) => String(value.id) === String(item.id) ? updated : value);
        if (result?.student) {
          const student = normalizeAdminStudent(result.student);
          state.adminStudents = [student, ...state.adminStudents.filter((value) => String(value.id) !== String(student.id))];
        }
        renderAdmin();
        showToast(result?.alreadyApproved ? I18N.t('admin.admissionAlreadyApproved') : I18N.t('admin.admissionApproved'));
      } catch (error) {
        setButtonLoading(button, false, 'admin.admissionApprove');
        if (isAuthError(error)) redirectToLogin(); else showToast(localizedError(error, 'admin.error'));
      }
    });
  };

  const handleAdminNotifications = () => {
    const list = document.querySelector('[data-admin-notification-list]');
    if (!list) return;
    list.addEventListener('click', async (event) => {
      const button = event.target.closest('[data-admin-notification-read]');
      if (!button) return;
      const item = state.adminNotifications.find((value) => String(value.id) === String(button.dataset.adminNotificationRead));
      if (!item || item.readAt) return;
      button.disabled = true;
      try {
        await adminRequest(`/admin/notifications/${encodeURIComponent(item.id)}`, 'PATCH', { read: true });
        item.readAt = new Date().toISOString();
        state.adminNotificationUnread = Math.max(0, (Number(state.adminNotificationUnread) || 1) - 1);
        renderAdminNotifications();
        showToast(I18N.t('admin.notificationMarkedRead'));
      } catch (error) {
        button.disabled = false;
        if (isAuthError(error)) redirectToLogin(); else showToast(I18N.t('admin.error'));
      }
    });
  };

  const mailboxPayload = (form) => {
    const values = Object.fromEntries(new FormData(form).entries());
    return { studentId: values.studentId?.trim(), subject: values.subject?.trim(), body: values.body?.trim(), external: form.elements.external?.checked === true, idempotencyKey: form.dataset.mailboxRequestKey || '' };
  };

  const handleAdminMailbox = () => {
    const form = document.querySelector('[data-mailbox-form]');
    if (!form) return;
    const submit = form.querySelector('button[type="submit"]');
    form.addEventListener('submit', async (event) => {
      event.preventDefault();
      if (!form.checkValidity()) { showToast(I18N.t('admin.required')); form.reportValidity(); return; }
      form.dataset.mailboxRequestKey ||= newRequestKey();
      setButtonLoading(submit, true, 'admin.sendingMessage');
      try {
        const result = await adminRequest('/admin/mailbox', 'POST', mailboxPayload(form));
        const message = normalizeAdminMailbox(result?.message || result);
        if (result?.replayed) {
          state.adminMailbox = state.adminMailbox.map((item) => String(item.id) === String(message.id) ? message : item);
          if (!state.adminMailbox.some((item) => String(item.id) === String(message.id))) state.adminMailbox.unshift(message);
        } else {
          state.adminMailbox.unshift(message);
        }
        delete form.dataset.mailboxRequestKey;
        form.reset();
        renderAdmin();
        showToast(message.deliveryStatus === 'sent' ? I18N.t('admin.smtpSent') : message.deliveryStatus === 'failed' ? I18N.t('admin.smtpFailed') : message.deliveryStatus === 'unknown' ? I18N.t('admin.smtpUnknown') : message.deliveryStatus === 'not_configured' ? I18N.t('admin.smtpNotConfigured') : result?.replayed ? I18N.t('admin.messageAlreadyRecorded') : I18N.t('admin.messageSent'));
      } catch (error) {
        if (error.details?.id) {
          const details = normalizeAdminMailbox(error.details);
          state.adminMailbox = [details, ...state.adminMailbox.filter((item) => String(item.id) !== String(details.id))];
          renderAdminMailbox();
        }
        if (isAuthError(error)) redirectToLogin();
        else showToast(localizedError(error, 'admin.error'));
      } finally {
        setButtonLoading(submit, false, 'admin.sendMessage');
      }
    });
    document.querySelector('[data-admin-mailbox-list]')?.addEventListener('click', async (event) => {
      const button = event.target.closest('[data-retry-mailbox]');
      if (!button) return;
      const id = button.dataset.retryMailbox;
      const confirmUnknown = button.dataset.retryConfirm === 'true' && window.confirm(I18N.t('admin.confirmUnknownDelivery'));
      if (button.dataset.retryConfirm === 'true' && !confirmUnknown) return;
      button.disabled = true;
      try {
        const result = await adminRequest(`/admin/mailbox/${encodeURIComponent(id)}/retry`, 'POST', { confirmUnknown });
        const updated = normalizeAdminMailbox(result?.message || result);
        state.adminMailbox = state.adminMailbox.map((item) => String(item.id) === String(id) ? updated : item);
        renderAdmin();
        showToast(updated.deliveryStatus === 'sent' ? I18N.t('admin.smtpSent') : updated.deliveryStatus === 'unknown' ? I18N.t('admin.smtpUnknown') : updated.deliveryStatus === 'not_configured' ? I18N.t('admin.smtpNotConfigured') : I18N.t('admin.smtpFailed'));
      } catch (error) {
        button.disabled = false;
        if (error.details?.id) {
          const details = normalizeAdminMailbox(error.details);
          state.adminMailbox = state.adminMailbox.map((item) => String(item.id) === String(details.id) ? details : item);
          renderAdminMailbox();
        }
        if (isAuthError(error)) redirectToLogin(); else showToast(localizedError(error, 'admin.error'));
      }
    });
  };

  const studentPayload = (form) => {
    const values = Object.fromEntries(new FormData(form).entries());
    const payload = {
      id: values.id?.trim() || undefined,
      username: values.username?.trim(),
      name: values.name?.trim(),
      email: values.email?.trim() || '',
      studentId: values.studentId?.trim(),
      college: values.college?.trim() || '',
      year: values.year?.trim() || '',
      active: form.elements.active ? form.elements.active.checked : true
    };
    if (values.password?.length) payload.password = values.password;
    return payload;
  };

  const academicValue = (value) => String(value ?? '').trim() === '' ? null : String(value).trim();
  const gradePayload = (form) => {
    const values = Object.fromEntries(new FormData(form).entries());
    return {
      id: values.id?.trim() || undefined,
      studentId: values.studentId,
      courseId: values.courseId,
      score: academicValue(values.score),
      point: academicValue(values.point),
      term: values.term?.trim() || '',
      credits: numberValue(values.credits, 0),
      status: values.status || 'inprogress'
    };
  };

  const schedulePayload = (form) => {
    const values = Object.fromEntries(new FormData(form).entries());
    return {
      id: values.id?.trim() || undefined,
      studentId: values.studentId,
      courseId: values.courseId,
      day: numberValue(values.day, 1),
      start: values.start,
      end: values.end,
      location: values.location?.trim() || '',
      teacher: values.teacher?.trim() || ''
    };
  };

  const handleAdminStudents = () => {
    const form = document.querySelector('[data-student-form]');
    const editor = document.querySelector('[data-student-editor]');
    if (!form) return;
    form.addEventListener('submit', async (event) => {
      event.preventDefault();
      if (!form.checkValidity()) { showToast(I18N.t('admin.required')); form.reportValidity(); return; }
      const values = studentPayload(form); const id = form.elements.id.value;
      if (!id && !values.password) { showToast(I18N.t('admin.studentPasswordRequired')); form.elements.password.focus(); return; }
      if (!beginAdminFormSubmit(form)) return;
      try {
        if (id) {
          const result = await adminRequest(`/admin/students/${encodeURIComponent(id)}`, 'PATCH', values);
          const updated = normalizeAdminStudent(result?.student || result || { ...values, id });
          state.adminStudents = state.adminStudents.map((item) => String(item.id) === String(id) ? updated : item);
        } else {
          const result = await adminRequest('/admin/students', 'POST', values);
          state.adminStudents.unshift(normalizeAdminStudent(result?.student || result || values));
        }
        form.reset(); form.elements.id.value = ''; showEditor(editor, false); renderAdmin(); showToast(I18N.t('admin.saved'));
      } catch (error) {
        if (isAuthError(error)) redirectToLogin();
        else if (error.code === 'student_identity_immutable') showToast(I18N.t('admin.studentIdentityLocked'));
        else showToast(localizedError(error, 'admin.error'));
      }
      finally { endAdminFormSubmit(form); }
    });
    document.querySelector('[data-new-student]')?.addEventListener('click', () => { form.reset(); form.elements.id.value = ''; form.elements.studentId.readOnly = false; form.elements.password.required = true; showEditor(editor, true); });
    document.querySelector('[data-cancel-student]')?.addEventListener('click', () => { form.reset(); form.elements.id.value = ''; form.elements.studentId.readOnly = false; form.elements.password.required = false; showEditor(editor, false); });
    document.querySelector('[data-admin-student-list]')?.addEventListener('click', async (event) => {
      const edit = event.target.closest('[data-edit-student]');
      if (edit) {
        const item = adminStudent(edit.dataset.editStudent);
        if (!item) return;
        fillForm(form, item, ['id', 'username', 'name', 'email', 'studentId', 'college', 'year', 'active']);
        form.elements.studentId.readOnly = Boolean(item.admissionApproved);
        form.elements.studentId.title = item.admissionApproved ? I18N.t('admin.studentIdentityLocked') : '';
        form.elements.password.value = '';
        form.elements.password.required = false;
        showEditor(editor, true);
        return;
      }
      const toggle = event.target.closest('[data-toggle-student]');
      if (toggle) {
        const item = adminStudent(toggle.dataset.toggleStudent);
        if (!item) return;
        const nextActive = !item.active;
        if (!window.confirm(I18N.t(nextActive ? 'admin.studentEnableConfirm' : 'admin.studentDisableConfirm'))) return;
        try {
          const result = await adminRequest(`/admin/students/${encodeURIComponent(item.id)}`, 'PATCH', { active: nextActive });
          const updated = normalizeAdminStudent(result?.student || result || { ...item, active: nextActive });
          state.adminStudents = state.adminStudents.map((value) => String(value.id) === String(item.id) ? updated : value);
          renderAdmin();
          showToast(I18N.t(nextActive ? 'admin.studentEnabled' : 'admin.studentDisabled'));
        } catch (error) {
          if (isAuthError(error)) redirectToLogin(); else showToast(localizedError(error, 'admin.error'));
        }
      }
    });
  };

  const handleAdminGrades = () => {
    const form = document.querySelector('[data-grade-form]');
    const editor = document.querySelector('[data-grade-editor]');
    const list = document.querySelector('[data-admin-grade-list]');
    if (!form || !list) return;
    form.addEventListener('submit', async (event) => {
      event.preventDefault();
      if (!form.checkValidity()) { showToast(I18N.t('admin.required')); form.reportValidity(); return; }
      const values = gradePayload(form); const id = form.elements.id.value;
      if (!beginAdminFormSubmit(form)) return;
      try {
        if (id) {
          const result = await adminRequest(`/admin/grades/${encodeURIComponent(id)}`, 'PATCH', values);
          const updated = normalizeAdminGrade(result?.grade || result || { ...values, id });
          state.adminGrades = state.adminGrades.map((item) => String(item.id) === String(id) ? updated : item);
        } else {
          const result = await adminRequest('/admin/grades', 'POST', values);
          state.adminGrades.unshift(normalizeAdminGrade(result?.grade || result || values));
        }
        form.reset(); form.elements.id.value = ''; showEditor(editor, false); renderAdmin(); showToast(I18N.t('admin.saved'));
      } catch (error) { if (isAuthError(error)) redirectToLogin(); else showToast(localizedError(error, 'admin.error')); }
      finally { endAdminFormSubmit(form); }
    });
    document.querySelector('[data-new-grade]')?.addEventListener('click', () => { form.reset(); form.elements.id.value = ''; renderAdminReferenceOptions(); showEditor(editor, true); });
    document.querySelector('[data-cancel-grade]')?.addEventListener('click', () => { form.reset(); form.elements.id.value = ''; showEditor(editor, false); });
    list.addEventListener('click', async (event) => {
      const edit = event.target.closest('[data-edit-grade]'); const remove = event.target.closest('[data-delete-grade]');
      if (edit) {
        const item = state.adminGrades.find((value) => String(value.id) === String(edit.dataset.editGrade));
        if (item) { fillForm(form, { ...item, score: item.score || '', point: item.point || '' }, ['id', 'studentId', 'courseId', 'score', 'point', 'term', 'credits', 'status']); renderAdminReferenceOptions(); showEditor(editor, true); }
      }
      if (remove) {
        const item = state.adminGrades.find((value) => String(value.id) === String(remove.dataset.deleteGrade));
        if (!item || !window.confirm(I18N.t('admin.deleteConfirm'))) return;
        try { await adminRequest(`/admin/grades/${encodeURIComponent(item.id)}`, 'DELETE'); state.adminGrades = state.adminGrades.filter((value) => String(value.id) !== String(item.id)); renderAdmin(); showToast(I18N.t('admin.deleted')); } catch (error) { if (isAuthError(error)) redirectToLogin(); else showToast(localizedError(error, 'admin.error')); }
      }
    });
  };

  const handleAdminSchedule = () => {
    const form = document.querySelector('[data-schedule-form]');
    const editor = document.querySelector('[data-schedule-editor]');
    const list = document.querySelector('[data-admin-schedule-list]');
    if (!form || !list) return;
    form.addEventListener('submit', async (event) => {
      event.preventDefault();
      if (!form.checkValidity()) { showToast(I18N.t('admin.required')); form.reportValidity(); return; }
      const values = schedulePayload(form); const id = form.elements.id.value;
      if (!beginAdminFormSubmit(form)) return;
      try {
        if (id) {
          const result = await adminRequest(`/admin/schedule/${encodeURIComponent(id)}`, 'PATCH', values);
          const updated = normalizeAdminSchedule(result?.schedule || result || { ...values, id });
          state.adminSchedule = state.adminSchedule.map((item) => String(item.id) === String(id) ? updated : item);
        } else {
          const result = await adminRequest('/admin/schedule', 'POST', values);
          state.adminSchedule.unshift(normalizeAdminSchedule(result?.schedule || result || values));
        }
        form.reset(); form.elements.id.value = ''; showEditor(editor, false); renderAdmin(); showToast(I18N.t('admin.saved'));
      } catch (error) { if (isAuthError(error)) redirectToLogin(); else showToast(localizedError(error, 'admin.error')); }
      finally { endAdminFormSubmit(form); }
    });
    document.querySelector('[data-new-schedule]')?.addEventListener('click', () => { form.reset(); form.elements.id.value = ''; renderAdminReferenceOptions(); showEditor(editor, true); });
    document.querySelector('[data-cancel-schedule]')?.addEventListener('click', () => { form.reset(); form.elements.id.value = ''; showEditor(editor, false); });
    list.addEventListener('click', async (event) => {
      const edit = event.target.closest('[data-edit-schedule]'); const remove = event.target.closest('[data-delete-schedule]');
      if (edit) {
        const item = state.adminSchedule.find((value) => String(value.id) === String(edit.dataset.editSchedule));
        if (item) { fillForm(form, item, ['id', 'studentId', 'courseId', 'day', 'start', 'end', 'location', 'teacher']); renderAdminReferenceOptions(); showEditor(editor, true); }
      }
      if (remove) {
        const item = state.adminSchedule.find((value) => String(value.id) === String(remove.dataset.deleteSchedule));
        if (!item || !window.confirm(I18N.t('admin.deleteConfirm'))) return;
        try { await adminRequest(`/admin/schedule/${encodeURIComponent(item.id)}`, 'DELETE'); state.adminSchedule = state.adminSchedule.filter((value) => String(value.id) !== String(item.id)); renderAdmin(); showToast(I18N.t('admin.deleted')); }
        catch (error) { if (isAuthError(error)) redirectToLogin(); else showToast(localizedError(error, 'admin.error')); }
      }
    });
  };

  const loadAdminDataNow = async () => {
    const loadVersion = state.adminMutationVersion;
    const requests = [
      resource('/admin/courses', ['courses']),
      resource('/admin/announcements', ['announcements', 'items']),
      optionalResource('/admin/admissions', ['applications', 'admissions', 'items']),
      optionalResource('/admin/students', ['students', 'items']),
      optionalResource('/admin/grades', ['grades', 'items']),
      optionalResource('/admin/schedule', ['schedule', 'entries', 'items']),
      optionalResource('/admin/mailbox', ['messages', 'items']),
      api('/admin/notifications'),
      api('/admin/stats'),
      resource('/admin/site-content', ['content', 'items'])
    ];
    const results = await Promise.allSettled(requests);
    const authFailure = results.find((result) => result.status === 'rejected' && isAuthError(result.reason));
    if (authFailure) {
      redirectToLogin();
      return false;
    }
    // A mutation began while these requests were in flight. Its handlers own
    // the optimistic local update; discard this stale snapshot instead of
    // overwriting it with pre-mutation data.
    if (loadVersion !== state.adminMutationVersion) return false;
    const valueAt = (index, fallback) => results[index]?.status === 'fulfilled' ? results[index].value : fallback;
    const courses = valueAt(0, state.adminCourses);
    const announcements = valueAt(1, state.adminAnnouncements);
    const admissions = valueAt(2, state.adminAdmissions);
    const students = valueAt(3, state.adminStudents);
    const grades = valueAt(4, state.adminGrades);
    const schedule = valueAt(5, state.adminSchedule);
    const mailbox = valueAt(6, state.adminMailbox);
    const notifications = valueAt(7, { notifications: state.adminNotifications, unread: state.adminNotificationUnread });
    const stats = valueAt(8, state.adminStats);
    const content = valueAt(9, state.adminSiteContent);
    const managed = new Map((I18N.catalog?.() || []).map((item) => [item.key, normalizeSiteContent(item)]));
    (Array.isArray(content) ? content : []).map(normalizeSiteContent).filter((item) => item.key !== 'common.loading').forEach((item) => {
      const existing = managed.get(item.key) || {};
      managed.set(item.key, {
        ...existing,
        ...item,
        zh: typeof item.zh === 'string' ? item.zh : (existing.zh || ''),
        en: typeof item.en === 'string' ? item.en : (existing.en || '')
      });
    });
    const previousAdmissions = new Map(state.adminAdmissions.map((item) => [String(item.id), item]));
    state.adminCourses = (Array.isArray(courses) ? courses : []).map(normalizeCourse);
    state.adminAnnouncements = (Array.isArray(announcements) ? announcements : []).map(normalizeAnnouncement);
    state.adminAdmissions = (Array.isArray(admissions) ? admissions : []).map(normalizeAdmission).map((item) => {
      const previous = previousAdmissions.get(String(item.id));
      return item.issuedCredentials || !previous?.issuedCredentials ? item : { ...item, issuedCredentials: previous.issuedCredentials };
    });
    state.adminStudents = (Array.isArray(students) ? students : []).map(normalizeAdminStudent);
    state.adminGrades = (Array.isArray(grades) ? grades : []).map(normalizeAdminGrade);
    state.adminSchedule = (Array.isArray(schedule) ? schedule : []).map(normalizeAdminSchedule);
    state.adminMailbox = (Array.isArray(mailbox) ? mailbox : []).map(normalizeAdminMailbox);
    state.adminNotifications = (Array.isArray(notifications?.notifications) ? notifications.notifications : []).map(normalizeAdminNotification);
    state.adminNotificationUnread = Number(notifications?.unread) || 0;
    state.adminSiteContent = [...managed.values()].filter((item) => item.key).sort((a, b) => a.key.localeCompare(b.key));
    state.adminStats = stats || {};
    renderAdmin();
    showPageAlert(results.some((result) => result.status === 'rejected') ? I18N.t('admin.loadPartialError') : '');
    return true;
  };

  const loadAdminData = async () => {
    if (state.adminLoading) return;
    state.adminLoading = true;
    const refresh = document.querySelector('[data-admin-refresh]');
    setButtonLoading(refresh, true, 'admin.refreshing');
    try {
      // A single retry is enough to converge after a mutation invalidates the
      // first snapshot, while keeping a degraded backend from spinning.
      for (let attempt = 0; attempt < 2; attempt += 1) {
        if (await loadAdminDataNow()) break;
      }
    } finally {
      state.adminLoading = false;
      setButtonLoading(refresh, false, 'admin.refresh');
    }
  };

  const startAdminRefresh = () => {
    if (state.adminRefreshTimer) return;
    // Keep the open admin tab current without interrupting hidden/background
    // tabs. The manual button remains available for an immediate refresh.
    state.adminRefreshTimer = window.setInterval(() => {
      if (document.visibilityState === 'visible') loadAdminData();
    }, 15000);
    document.addEventListener('visibilitychange', () => {
      if (document.visibilityState === 'visible') loadAdminData();
    });
    window.addEventListener('beforeunload', () => {
      window.clearInterval(state.adminRefreshTimer);
      state.adminRefreshTimer = 0;
    }, { once: true });
  };

  const initAdmin = async () => {
    setupHashNav('admin');
    handleAdminCourseForm(); handleAdminAnnouncementForm(); handleAdminSiteContent(); handleAdminAdmissions(); handleAdminNotifications(); handleAdminMailbox(); handleAdminStudents(); handleAdminGrades(); handleAdminSchedule();
    document.querySelector('[data-admin-refresh]')?.addEventListener('click', () => loadAdminData());
    // Bind every editor before waiting on i18n/auth/data. This keeps actions
    // deterministic when an API is slow and ensures a failed list request
    // cannot strand the page with inert "新增" buttons.
    await waitForManagedContent();
    if (!await ensureSession('admin')) return;
    await loadAdminData();
    startAdminRefresh();
    window.addEventListener('cgu:localechange', renderAdmin);
  };

  document.addEventListener('DOMContentLoaded', () => {
    setupCommon();
    if (page === 'login') handleLogin();
    if (page === 'portal') initPortal();
    if (page === 'admin') initAdmin();
  });
})();
