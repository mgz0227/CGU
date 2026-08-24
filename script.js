(() => {
  const header = document.querySelector('[data-header]');
  const menuToggle = document.querySelector('.menu-toggle');
  const mobileMenu = document.querySelector('#mobile-menu');
  const dialog = document.querySelector('#apply-dialog');
  const toast = document.querySelector('[data-toast]');
  const year = document.querySelector('[data-year]');
  let toastTimer;

  if (year) year.textContent = new Date().getFullYear();

  const setMenu = (open) => {
    if (!menuToggle || !mobileMenu) return;
    menuToggle.setAttribute('aria-expanded', String(open));
    menuToggle.setAttribute('aria-label', open ? '关闭菜单' : '打开菜单');
    mobileMenu.classList.toggle('is-open', open);
    mobileMenu.setAttribute('aria-hidden', String(!open));
    document.body.classList.toggle('menu-open', open);
  };

  menuToggle?.addEventListener('click', () => setMenu(menuToggle.getAttribute('aria-expanded') !== 'true'));
  mobileMenu?.querySelectorAll('a').forEach((link) => link.addEventListener('click', () => setMenu(false)));

  const onScroll = () => header?.classList.toggle('is-scrolled', window.scrollY > 24);
  onScroll();
  window.addEventListener('scroll', onScroll, { passive: true });

  const filters = document.querySelectorAll('[data-filter]');
  const cards = document.querySelectorAll('.program-card');
  filters.forEach((filterButton) => {
    filterButton.addEventListener('click', () => {
      const filter = filterButton.dataset.filter;
      filters.forEach((button) => {
        const active = button === filterButton;
        button.classList.toggle('is-active', active);
        button.setAttribute('aria-selected', String(active));
      });
      cards.forEach((card) => card.classList.toggle('is-hidden', filter !== 'all' && card.dataset.region !== filter));
    });
  });

  const showToast = (message) => {
    if (!toast) return;
    toast.textContent = message;
    toast.classList.add('is-visible');
    window.clearTimeout(toastTimer);
    toastTimer = window.setTimeout(() => toast.classList.remove('is-visible'), 4200);
  };

  const openDialog = () => {
    if (dialog?.showModal) dialog.showModal();
    else dialog?.setAttribute('open', '');
  };
  const closeDialog = () => dialog?.close?.();
  document.querySelectorAll('[data-open-apply]').forEach((button) => button.addEventListener('click', openDialog));
  document.querySelector('[data-close-dialog]')?.addEventListener('click', closeDialog);
  dialog?.addEventListener('click', (event) => {
    if (event.target === dialog) closeDialog();
  });

  document.querySelector('.apply-form')?.addEventListener('submit', (event) => {
    event.preventDefault();
    const form = event.currentTarget;
    if (!form.checkValidity()) {
      form.reportValidity();
      return;
    }
    closeDialog();
    form.reset();
    showToast('申请意向已收到，招生老师会尽快与你联系。');
  });

  document.querySelectorAll('.program-detail').forEach((button) => {
    button.addEventListener('click', () => showToast(`${button.dataset.program}：课程详情将在招生简章中展开。`));
  });

  const sections = [...document.querySelectorAll('main section[id]')];
  const navLinks = [...document.querySelectorAll('.desktop-nav a')];
  if ('IntersectionObserver' in window && navLinks.length) {
    const observer = new IntersectionObserver((entries) => {
      entries.forEach((entry) => {
        if (!entry.isIntersecting) return;
        navLinks.forEach((link) => link.classList.toggle('is-active', link.getAttribute('href') === `#${entry.target.id}`));
      });
    }, { rootMargin: '-35% 0px -55% 0px', threshold: 0 });
    sections.forEach((section) => observer.observe(section));
  }
})();
