(() => {
  'use strict';

  const page = document.body?.dataset.page;
  const I18N = window.CGU_I18N || {
    locale: 'zh',
    t: (key) => key,
    pick: (value, fallback = '') => value ?? fallback,
    apply: () => {}
  };
  const API_BASE = String(window.CGU_CONFIG?.apiBase || '/api').replace(/\/$/, '');
  const USER_KEY = 'cgu_user';
  const DEMO_KEY = 'cgu_demo_mode';

  const demoCourses = [
    { id: 'wind-101', code: 'CGU-W101', nameZh: '风与自然科学', nameEn: 'Wind & Natural Sciences', teacher: '琴 / Jean', credits: 4, term: '2026 秋季', type: 'required', capacity: 80, enrolledCount: 52, enrolled: true, description: '研究风场、生态与自由意志的边界。' },
    { id: 'contract-204', code: 'CGU-C204', nameZh: '契约与商业文明', nameEn: 'Contracts & Commerce', teacher: '凝光 / Ningguang', credits: 3, term: '2026 秋季', type: 'required', capacity: 60, enrolledCount: 60, enrolled: false, description: '从港口出发，理解秩序、交换与信任。' },
    { id: 'eternity-110', code: 'CGU-E110', nameZh: '永恒与设计实践', nameEn: 'Eternity & Design Practice', teacher: '神里绫华 / Ayaka', credits: 3, term: '2026 秋季', type: 'elective', capacity: 45, enrolledCount: 29, enrolled: false, description: '在变化中寻找稳定，在限制中创造新意。' },
    { id: 'wisdom-301', code: 'CGU-W301', nameZh: '智慧与生命研究', nameEn: 'Wisdom & Life Studies', teacher: '纳西妲 / Nahida', credits: 4, term: '2027 春季', type: 'elective', capacity: 40, enrolledCount: 18, enrolled: false, description: '让知识走出终端，回到真实的生命现场。' },
    { id: 'stars-220', code: 'CGU-S220', nameZh: '星象观测与测绘', nameEn: 'Astral Cartography', teacher: '莫娜 / Mona', credits: 3, term: '2026 秋季', type: 'elective', capacity: 30, enrolledCount: 17, enrolled: false, description: '用星图记录每一条尚未抵达的路线。' },
    { id: 'fontaine-310', code: 'CGU-F310', nameZh: '审判与机械文明', nameEn: 'Judgment & Mechanical Civilization', teacher: '那维莱特 / Neuvillette', credits: 4, term: '2026 秋季', type: 'required', capacity: 40, enrolledCount: 21, enrolled: false, description: '从法庭与工坊，研究规则、能源与机械创造。' },
    { id: 'natlan-220', code: 'CGU-N220', nameZh: '火与竞技生态', nameEn: 'Fire & Competitive Ecology', teacher: '教务联合授课', credits: 3, term: '2026 秋季', type: 'elective', capacity: 36, enrolledCount: 16, enrolled: false, description: '在部族、仪式与竞技场之间完成田野研究。' },
    { id: 'snezhnaya-401', code: 'CGU-S401', nameZh: '至冬研究与极地治理', nameEn: 'Snezhnaya Studies & Polar Governance', teacher: '教务联合授课', credits: 4, term: '2026 秋季', type: 'elective', capacity: 32, enrolledCount: 12, enrolled: false, description: '以 7.0「无神怜爱的雪国」为起点，研究冰原社会与远行伦理。' }
  ];
  const demoGrades = [
    { id: 'g1', courseId: 'wind-101', courseCode: 'CGU-W101', courseNameZh: '风与自然科学', courseNameEn: 'Wind & Natural Sciences', score: 92, point: 4, term: '2026 春季', status: 'passed', credits: 4 },
    { id: 'g2', courseId: 'history-101', courseCode: 'CGU-H101', courseNameZh: '提瓦特文明导论', courseNameEn: 'Introduction to Teyvat Civilisation', score: 88, point: 3.7, term: '2026 春季', status: 'passed', credits: 3 },
    { id: 'g3', courseId: 'field-108', courseCode: 'CGU-F108', courseNameZh: '野外实践基础', courseNameEn: 'Field Practice Fundamentals', score: '—', point: '—', term: '2026 秋季', status: 'inProgress', credits: 2 }
  ];
  const demoSchedule = [
    { id: 's1', day: 1, start: '09:00', end: '10:40', courseId: 'wind-101', courseCode: 'CGU-W101', courseNameZh: '风与自然科学', courseNameEn: 'Wind & Natural Sciences', location: '风之庭院 A-201' },
    { id: 's2', day: 3, start: '14:00', end: '15:40', courseId: 'contract-204', courseCode: 'CGU-C204', courseNameZh: '契约与商业文明', courseNameEn: 'Contracts & Commerce', location: '璃月港 B-108' },
    { id: 's3', day: 5, start: '10:00', end: '11:40', courseId: 'eternity-110', courseCode: 'CGU-E110', courseNameZh: '永恒与设计实践', courseNameEn: 'Eternity & Design Practice', location: '稻妻工坊 C-03' },
    { id: 's4', day: 2, start: '16:00', end: '17:40', courseId: 'stars-220', courseCode: 'CGU-S220', courseNameZh: '星象观测与测绘', courseNameEn: 'Astral Cartography', location: '须弥穹顶观测台' }
  ];
  const demoAnnouncements = [
    { id: 'a1', type: 'ADMISSIONS', titleZh: '2026 秋季选课窗口开放', titleEn: 'Autumn 2026 course selection is open', contentZh: '请在 9 月 12 日前完成课程确认，冲突课程将由教务处统一复核。', contentEn: 'Confirm your courses by 12 September. Conflicts will be reviewed by the registrar.', publishedAt: '2026-08-18T09:00:00+08:00', published: true },
    { id: 'a2', type: 'CAMPUS', titleZh: '风之庭院夜间自习区开放', titleEn: 'Windrise evening study hall is open', contentZh: '蒙德校区图书馆一层延长开放至 23:00，请携带学生证入场。', contentEn: 'The Mondstadt library first floor is open until 23:00. Bring your student card.', publishedAt: '2026-08-05T10:00:00+08:00', published: true },
    { id: 'a3', type: 'RESEARCH', titleZh: '「元素与城市」研究招募', titleEn: 'Element & City research call', contentZh: '跨学院研究团队正在招募对城市、能源与文化感兴趣的学生。', contentEn: 'The interdisciplinary team welcomes students interested in cities, energy, and culture.', publishedAt: '2026-07-24T14:30:00+08:00', published: true },
    { id: 'a4', type: 'WORLD_UPDATE', titleZh: '7.0「无神怜爱的雪国」：至冬研究方向开放', titleEn: 'Version 7.0 “Everwinter Without Mercy”: Snezhnaya studies open', contentZh: '根据原神官方 7.0 版本资讯，CGU 新增至冬研究与极地治理课程。', contentEn: 'Following the official Version 7.0 update, CGU adds a Snezhnaya studies track.', publishedAt: '2026-08-24T08:00:00+08:00', published: true }
  ];

  const state = {
    user: null,
    demo: false,
    dataFallback: false,
    courses: [],
    enrollments: [],
    grades: [],
    schedule: [],
    announcements: [],
    adminCourses: [],
    adminAnnouncements: [],
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
  const clearSession = () => { writeStorage(USER_KEY, ''); writeStorage(DEMO_KEY, ''); };

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
  const isMissingService = (error) => error?.network || error?.status === 404 || error?.status === 405 || error?.status >= 500;

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
        state.demo = false;
        writeStorage(DEMO_KEY, '');
        saveUser(user);
        redirectAfterLogin(user);
      } catch (error) {
        const normalizedUsername = values.username.trim().toLowerCase();
        const isDemo = (normalizedUsername === 'student' && values.password === 'student-demo') || (normalizedUsername === 'admin' && values.password === 'admin-demo');
        if (isMissingService(error) && isDemo) {
          const user = normalizeUser(values.username.trim().toLowerCase() === 'admin' ? { id: 'admin', username: 'admin', name: 'CGU Admin', role: 'admin', email: 'admin@cgu.example' } : { id: 'CGU2026001', username: 'student', name: '旅行者', role: 'student', studentId: 'CGU2026001', email: 'traveler@cgu.example', college: '风与自然科学学院', year: '2026' });
          state.user = user;
          state.demo = true;
          writeStorage(DEMO_KEY, '1');
          saveUser(user);
          redirectAfterLogin(user);
        } else if (error.status === 401 || error.status === 403) setError('login.errorInvalid');
        else setError(error.network || error.status >= 500 ? 'login.errorUnavailable' : 'login.errorInvalid');
      } finally {
        setButtonLoading(submit, false, 'login.submit');
      }
    });
    const existing = readUser();
    if (existing || readStorage(DEMO_KEY)) {
      apiAny(['/auth/me', '/me']).then((value) => { const user = normalizeUser(value); saveUser(user); redirectAfterLogin(user); }).catch((error) => {
        if (readStorage(DEMO_KEY) && error.network) redirectAfterLogin(normalizeUser(existing));
        else if (isAuthError(error)) clearSession();
      });
    }
  };

  const ensureSession = async (requiredRole = 'student') => {
    const storedUser = readUser();
    const demo = readStorage(DEMO_KEY) === '1';
    try {
      const value = await apiAny(['/auth/me', '/me']);
      state.user = normalizeUser(value);
      state.demo = false;
      saveUser(state.user);
    } catch (error) {
      if (isAuthError(error)) { redirectToLogin(); return false; }
      if (demo && storedUser) { state.user = normalizeUser(storedUser); state.demo = true; }
      else { redirectToLogin(); return false; }
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
      try { if (!state.demo) await apiAny(['/auth/logout', '/logout'], { method: 'POST', body: '{}' }); } catch { /* local session is still cleared */ }
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

  const setActiveNav = (selector, section) => document.querySelectorAll(selector).forEach((link) => link.classList.toggle('is-active', link.dataset.portalNav === section || link.dataset.adminNav === section));
  const setupHashNav = (kind) => {
    const selector = kind === 'admin' ? '[data-admin-nav]' : '[data-portal-nav]';
    const getSection = () => (window.location.hash || '#overview').slice(1).replace('admin-', '') || 'overview';
    const update = () => setActiveNav(selector, getSection());
    window.addEventListener('hashchange', update);
    update();
    document.querySelectorAll(selector).forEach((link) => link.addEventListener('click', () => window.setTimeout(update, 0)));
  };

  const resource = async (path, fallback, keys) => {
    try { return listFrom(await api(path), keys); }
    catch (error) {
      if (isAuthError(error)) { redirectToLogin(); throw error; }
      if (isMissingService(error)) { state.dataFallback = true; return fallback.map((item) => ({ ...item })); }
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
    const notes = { credits: I18N.locale === 'zh' ? '本科阶段目标 120' : 'Undergraduate target 120', gpa: I18N.locale === 'zh' ? `${passed.length} 门已出分` : `${passed.length} graded courses`, enrolled: I18N.locale === 'zh' ? '本学期' : 'This term', nextClass: next ? `${dayLabel(next.day)} · ${next.start}` : '—' };
    Object.entries(metrics).forEach(([key, value]) => document.querySelectorAll(`[data-metric="${key}"]`).forEach((node) => { node.textContent = value; }));
    Object.entries(notes).forEach(([key, value]) => document.querySelectorAll(`[data-metric-note="${key}"]`).forEach((node) => { node.textContent = value; }));
    document.querySelectorAll('[data-grade-summary]').forEach((node) => { node.textContent = `${passed.length}/${state.grades.length}`; });
    document.querySelectorAll('[data-course-count]').forEach((node) => { node.textContent = String(state.courses.length).padStart(2, '0'); });
    document.querySelectorAll('[data-term-label]').forEach((node) => { node.textContent = state.courses[0]?.term || '2026'; });
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
      let response = null;
      if (state.demo || state.dataFallback) {
        course.enrolled = action === 'enroll';
        if (action === 'enroll') course.enrolledCount += 1;
        else course.enrolledCount = Math.max(0, course.enrolledCount - 1);
      } else {
        response = await postJSON('/enrollments', 'POST', { courseId: id, course_id: id, action });
        course.enrolled = action === 'enroll';
      }
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
        resource('/courses', demoCourses, ['courses']),
        resource('/enrollments', demoCourses.filter((course) => course.enrolled), ['enrollments', 'items']),
        resource('/grades', demoGrades, ['grades']),
        resource('/schedule', demoSchedule, ['schedule', 'entries']),
        resource('/announcements', demoAnnouncements, ['announcements', 'items'])
      ]);
      state.courses = courses.map(normalizeCourse);
      state.enrollments = enrollments;
      state.grades = grades.map(normalizeGrade);
      state.schedule = schedule.map(normalizeSchedule);
      state.announcements = announcements.map(normalizeAnnouncement);
      renderPortal();
      if (state.demo || state.dataFallback) showToast(I18N.t('common.demo'));
    } catch (error) {
      if (!isAuthError(error)) showPageAlert(I18N.t('portal.loadError'));
    } finally { setButtonLoading(refresh, false, 'portal.refresh'); }
  };

  const initPortal = async () => {
    setupHashNav('portal'); bindPortalEvents();
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
    I18N.apply();
  };

  const adminRequest = async (path, method, body) => {
    if (state.demo) {
      if (method === 'DELETE') return {};
      return body || {};
    }
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

  const loadAdminData = async () => {
    try {
      const [courses, announcements, stats] = await Promise.all([
        resource('/admin/courses', demoCourses, ['courses']),
        resource('/admin/announcements', demoAnnouncements, ['announcements', 'items']),
        api('/admin/stats').catch((error) => { if (isAuthError(error)) throw error; state.dataFallback = true; return {}; })
      ]);
      state.adminCourses = courses.map(normalizeCourse); state.adminAnnouncements = announcements.map(normalizeAnnouncement); state.adminStats = stats || {}; renderAdmin();
      if (state.demo) showToast(I18N.t('common.demo'));
    } catch (error) { if (!isAuthError(error)) showPageAlert(I18N.t('admin.error')); }
  };

  const initAdmin = async () => {
    setupHashNav('admin');
    if (!await ensureSession('admin')) return;
    handleAdminCourseForm(); handleAdminAnnouncementForm();
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
