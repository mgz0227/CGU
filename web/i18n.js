(() => {
  const STORAGE_KEY = 'cgu_locale';
  const dictionaries = {
    zh: {
      'brand.short': 'CGU',
      'brand.name': '原神大学',
      'brand.full': 'China Genshin University',
      'nav.home': '返回首页',
      'nav.portal': '学生教务',
      'nav.admin': '后台管理',
      'nav.logout': '退出登录',
      'nav.language': 'English',
      'nav.menu': '打开菜单',
      'nav.close': '关闭菜单',
      'login.pageTitle': 'CGU · 登录',
      'login.metaDescription': 'CGU 原神大学校园访问入口',
      'login.kicker': 'CHINA GENSHIN UNIVERSITY · CAMPUS ACCESS',
      'login.title': '登录 CGU 门户',
      'login.subtitle': '进入课程、成绩与校园服务。',
      'login.account': '账号',
      'login.accountPlaceholder': '学号或管理员账号',
      'login.password': '密码',
      'login.passwordPlaceholder': '请输入密码',
      'login.submit': '登录门户',
      'login.loading': '正在登录…',
      'login.back': '回到大学首页',
      'login.errorRequired': '请输入账号和密码。',
      'login.errorInvalid': '账号或密码不正确。',
      'login.errorUnavailable': '暂时无法连接教务服务，请稍后再试。',
      'login.footerCopyright': '© 2026 CGU',
      'login.footerServices': '教务服务',
      'common.skipLogin': '跳到登录表单',
      'common.skipMain': '跳到主内容',
      'common.loginPanel': '登录',
      'common.studentNav': '学生门户导航',
      'common.adminNav': '管理员导航',
      'common.mobileNav': '移动端导航',
      'common.termFilter': '学期',
      'common.courseTypeFilter': '课程类型',
      'admin.termPlaceholder': '2026 秋季',
      'portal.pageTitle': 'CGU · 学生教务',
      'portal.metaDescription': 'CGU 原神大学学生教务门户',
      'portal.kicker': 'STUDENT PORTAL',
      'portal.academicsKicker': 'ACADEMICS',
      'portal.campusKicker': 'CAMPUS LIFE',
      'portal.accountKicker': 'ACCOUNT',
      'portal.title': '我的教务空间',
      'portal.welcome': '欢迎回来，{name}',
      'portal.welcomeFallback': '欢迎回来',
      'portal.noticeType': 'NOTICE',
      'portal.refresh': '刷新数据',
      'portal.overview': '概览',
      'portal.courses': '课程中心',
      'portal.grades': '成绩单',
      'portal.schedule': '课表',
      'portal.announcements': '公告',
      'portal.profile': '个人资料',
      'portal.credits': '已修学分',
      'portal.gpa': '当前绩点',
      'portal.enrolled': '在修课程',
      'portal.nextClass': '下一节课',
      'portal.noNextClass': '今天没有安排',
      'portal.creditsTarget': '本科阶段目标 120',
      'portal.gradedCourses': '{count} 门已出分',
      'portal.currentTerm': '本学期',
      'portal.termFallback': '2026',
      'portal.courseSearch': '搜索课程、教师或学院',
      'portal.allTerms': '全部学期',
      'portal.allTypes': '全部类型',
      'portal.required': '必修',
      'portal.elective': '选修',
      'portal.enroll': '选课',
      'portal.drop': '退选',
      'portal.enrolledLabel': '已选',
      'portal.full': '已满',
      'portal.courseCode': '课程代码',
      'portal.courseName': '课程名称',
      'portal.teacher': '授课教师',
      'portal.creditsLabel': '学分',
      'portal.term': '学期',
      'portal.status': '状态',
      'portal.noCourses': '没有符合条件的课程。',
      'portal.gradeCourse': '课程',
      'portal.gradeScore': '成绩',
      'portal.gradePoint': '绩点',
      'portal.gradeTerm': '学期',
      'portal.gradeStatus': '状态',
      'portal.passed': '通过',
      'portal.inProgress': '进行中',
      'portal.noGrades': '暂无成绩记录。',
      'portal.today': '今天',
      'portal.mon': '周一',
      'portal.tue': '周二',
      'portal.wed': '周三',
      'portal.thu': '周四',
      'portal.fri': '周五',
      'portal.sat': '周六',
      'portal.sun': '周日',
      'portal.time': '时间',
      'portal.noSchedule': '暂无课表安排。',
      'portal.readMore': '查看详情',
      'portal.noAnnouncements': '暂无公告。',
      'portal.email': '邮箱',
      'portal.studentId': '学号',
      'portal.college': '学院',
      'portal.year': '年级',
      'portal.signOutConfirm': '确定要退出当前账号吗？',
      'portal.loadError': '数据加载失败，请点击“刷新数据”重试。',
      'portal.saved': '操作已完成。',
      'portal.enrollSuccess': '选课成功。',
      'portal.dropSuccess': '退选成功。',
      'portal.operationError': '操作失败，请稍后重试。',
      'admin.kicker': 'CGU ADMINISTRATION',
      'admin.pageTitle': 'CGU · 后台管理',
      'admin.metaDescription': 'CGU 原神大学教务管理后台',
      'admin.title': '教务管理台',
      'admin.subtitle': '维护课程、公告与校园学术信息。',
      'admin.overview': '总览',
      'admin.courses': '课程管理',
      'admin.announcements': '公告管理',
      'admin.siteContent': '网站内容',
      'admin.siteContentHelp': '编辑全站双语文案、图片地址与链接；保存后前台刷新即可生效。',
      'admin.contentKey': '内容键',
      'admin.contentKeyPlaceholder': '例如 home.heroTitleLead',
      'admin.zhValue': '中文内容',
      'admin.enValue': '英文内容',
      'admin.addContent': '新增内容',
      'admin.searchContent': '搜索键名或内容',
      'admin.noSiteContent': '暂无可编辑内容。',
      'admin.users': '用户概览',
      'admin.totalCourses': '课程总数',
      'admin.totalStudents': '学生总数',
      'admin.openSections': '开放班级',
      'admin.pending': '待处理事项',
      'admin.coursesUnit': 'COURSES',
      'admin.studentsUnit': 'STUDENTS',
      'admin.sectionsUnit': 'OPEN SECTIONS',
      'admin.pendingUnit': 'PENDING',
      'admin.courseQuickNumber': '01',
      'admin.announcementQuickNumber': '02',
      'admin.contentKicker': 'CONTENT MANAGEMENT',
      'admin.newCourse': '新增课程',
      'admin.editCourse': '编辑课程',
      'admin.newAnnouncement': '发布公告',
      'admin.editAnnouncement': '编辑公告',
      'admin.code': '课程代码',
      'admin.nameZh': '中文名称',
      'admin.nameEn': '英文名称',
      'admin.teacher': '授课教师',
      'admin.credits': '学分',
      'admin.term': '学期',
      'admin.capacity': '容量',
      'admin.type': '课程类型',
      'admin.description': '课程简介',
      'admin.save': '保存',
      'admin.cancel': '取消',
      'admin.edit': '编辑',
      'admin.delete': '删除',
      'admin.publish': '发布',
      'admin.unpublish': '撤回',
      'admin.titleZh': '中文标题',
      'admin.titleEn': '英文标题',
      'admin.content': '公告内容',
      'admin.publishedAt': '发布时间',
      'admin.actions': '操作',
      'admin.noCourses': '暂无课程。',
      'admin.noAnnouncements': '暂无公告。',
      'admin.deleteConfirm': '确定删除这条记录吗？',
      'admin.required': '请填写必填字段。',
      'admin.saved': '保存成功。',
      'admin.deleted': '删除成功。',
      'admin.error': '请求失败，请稍后重试。',
      'admin.accessDenied': '当前账号没有管理员权限。',
      'home.programDetailsSoon': '课程详情将在招生简章中展开。',
      'home.applicationReceived': '申请意向已收到，招生老师会尽快与你联系。',
      'common.loading': '加载中…',
      'common.error': '出错了',
      'common.close': '关闭',
      'common.retry': '重试',
      'common.empty': '暂无数据',
      'common.required': '必填'
    },
    en: {
      'brand.short': 'CGU',
      'brand.name': 'China Genshin University',
      'brand.full': 'China Genshin University',
      'nav.home': 'University home',
      'nav.portal': 'Student portal',
      'nav.admin': 'Administration',
      'nav.logout': 'Sign out',
      'nav.language': '中文',
      'nav.menu': 'Open menu',
      'nav.close': 'Close menu',
      'login.pageTitle': 'CGU · Sign in',
      'login.metaDescription': 'China Genshin University campus access',
      'login.kicker': 'CHINA GENSHIN UNIVERSITY · CAMPUS ACCESS',
      'login.title': 'Sign in to CGU',
      'login.subtitle': 'Access courses, grades, and campus services.',
      'login.account': 'Account',
      'login.accountPlaceholder': 'Student ID or admin account',
      'login.password': 'Password',
      'login.passwordPlaceholder': 'Enter your password',
      'login.submit': 'Sign in',
      'login.loading': 'Signing in…',
      'login.back': 'Back to university home',
      'login.errorRequired': 'Enter your account and password.',
      'login.errorInvalid': 'The account or password is incorrect.',
      'login.errorUnavailable': 'The academic service is temporarily unavailable.',
      'login.footerCopyright': '© 2026 CGU',
      'login.footerServices': 'Academic services',
      'common.skipLogin': 'Skip to sign-in form',
      'common.skipMain': 'Skip to main content',
      'common.loginPanel': 'Sign in',
      'common.studentNav': 'Student portal navigation',
      'common.adminNav': 'Administrator navigation',
      'common.mobileNav': 'Mobile navigation',
      'common.termFilter': 'Term',
      'common.courseTypeFilter': 'Course type',
      'admin.termPlaceholder': 'Autumn 2026',
      'portal.pageTitle': 'CGU · Student portal',
      'portal.metaDescription': 'China Genshin University student portal',
      'portal.kicker': 'STUDENT PORTAL',
      'portal.academicsKicker': 'ACADEMICS',
      'portal.campusKicker': 'CAMPUS LIFE',
      'portal.accountKicker': 'ACCOUNT',
      'portal.title': 'My academic space',
      'portal.welcome': 'Welcome back, {name}',
      'portal.welcomeFallback': 'Welcome back',
      'portal.noticeType': 'NOTICE',
      'portal.refresh': 'Refresh data',
      'portal.overview': 'Overview',
      'portal.courses': 'Course centre',
      'portal.grades': 'Grades',
      'portal.schedule': 'Schedule',
      'portal.announcements': 'Announcements',
      'portal.profile': 'Profile',
      'portal.credits': 'Credits earned',
      'portal.gpa': 'Current GPA',
      'portal.enrolled': 'Enrolled courses',
      'portal.nextClass': 'Next class',
      'portal.noNextClass': 'No classes today',
      'portal.creditsTarget': 'Undergraduate target 120',
      'portal.gradedCourses': '{count} graded courses',
      'portal.currentTerm': 'This term',
      'portal.termFallback': '2026',
      'portal.courseSearch': 'Search courses, teachers, or colleges',
      'portal.allTerms': 'All terms',
      'portal.allTypes': 'All types',
      'portal.required': 'Required',
      'portal.elective': 'Elective',
      'portal.enroll': 'Enroll',
      'portal.drop': 'Drop',
      'portal.enrolledLabel': 'Enrolled',
      'portal.full': 'Full',
      'portal.courseCode': 'Code',
      'portal.courseName': 'Course',
      'portal.teacher': 'Instructor',
      'portal.creditsLabel': 'Credits',
      'portal.term': 'Term',
      'portal.status': 'Status',
      'portal.noCourses': 'No courses match your filters.',
      'portal.gradeCourse': 'Course',
      'portal.gradeScore': 'Score',
      'portal.gradePoint': 'Point',
      'portal.gradeTerm': 'Term',
      'portal.gradeStatus': 'Status',
      'portal.passed': 'Passed',
      'portal.inProgress': 'In progress',
      'portal.noGrades': 'No grades yet.',
      'portal.today': 'Today',
      'portal.mon': 'Mon',
      'portal.tue': 'Tue',
      'portal.wed': 'Wed',
      'portal.thu': 'Thu',
      'portal.fri': 'Fri',
      'portal.sat': 'Sat',
      'portal.sun': 'Sun',
      'portal.time': 'Time',
      'portal.noSchedule': 'No schedule entries.',
      'portal.readMore': 'Read more',
      'portal.noAnnouncements': 'No announcements.',
      'portal.email': 'Email',
      'portal.studentId': 'Student ID',
      'portal.college': 'College',
      'portal.year': 'Year',
      'portal.signOutConfirm': 'Sign out of this account?',
      'portal.loadError': 'Could not load data. Try refreshing.',
      'portal.saved': 'Operation completed.',
      'portal.enrollSuccess': 'Enrollment completed.',
      'portal.dropSuccess': 'Course dropped.',
      'portal.operationError': 'Operation failed. Try again later.',
      'admin.kicker': 'CGU ADMINISTRATION',
      'admin.pageTitle': 'CGU · Administration',
      'admin.metaDescription': 'China Genshin University administration portal',
      'admin.title': 'Academic administration',
      'admin.subtitle': 'Maintain courses, announcements, and academic information.',
      'admin.overview': 'Overview',
      'admin.courses': 'Course management',
      'admin.announcements': 'Announcement management',
      'admin.siteContent': 'Website content',
      'admin.siteContentHelp': 'Edit bilingual site copy, image addresses, and links. Saved values apply after the frontend refreshes.',
      'admin.contentKey': 'Content key',
      'admin.contentKeyPlaceholder': 'For example home.heroTitleLead',
      'admin.zhValue': 'Chinese content',
      'admin.enValue': 'English content',
      'admin.addContent': 'Add content',
      'admin.searchContent': 'Search keys or content',
      'admin.noSiteContent': 'No editable content yet.',
      'admin.users': 'User overview',
      'admin.totalCourses': 'Total courses',
      'admin.totalStudents': 'Students',
      'admin.openSections': 'Open sections',
      'admin.pending': 'Pending items',
      'admin.coursesUnit': 'COURSES',
      'admin.studentsUnit': 'STUDENTS',
      'admin.sectionsUnit': 'OPEN SECTIONS',
      'admin.pendingUnit': 'PENDING',
      'admin.courseQuickNumber': '01',
      'admin.announcementQuickNumber': '02',
      'admin.contentKicker': 'CONTENT MANAGEMENT',
      'admin.newCourse': 'New course',
      'admin.editCourse': 'Edit course',
      'admin.newAnnouncement': 'Publish announcement',
      'admin.editAnnouncement': 'Edit announcement',
      'admin.code': 'Course code',
      'admin.nameZh': 'Chinese name',
      'admin.nameEn': 'English name',
      'admin.teacher': 'Instructor',
      'admin.credits': 'Credits',
      'admin.term': 'Term',
      'admin.capacity': 'Capacity',
      'admin.type': 'Type',
      'admin.description': 'Description',
      'admin.save': 'Save',
      'admin.cancel': 'Cancel',
      'admin.edit': 'Edit',
      'admin.delete': 'Delete',
      'admin.publish': 'Publish',
      'admin.unpublish': 'Unpublish',
      'admin.titleZh': 'Chinese title',
      'admin.titleEn': 'English title',
      'admin.content': 'Content',
      'admin.publishedAt': 'Published',
      'admin.actions': 'Actions',
      'admin.noCourses': 'No courses yet.',
      'admin.noAnnouncements': 'No announcements yet.',
      'admin.deleteConfirm': 'Delete this record?',
      'admin.required': 'Complete the required fields.',
      'admin.saved': 'Saved successfully.',
      'admin.deleted': 'Deleted successfully.',
      'admin.error': 'Request failed. Try again later.',
      'admin.accessDenied': 'This account does not have administrator access.',
      'home.programDetailsSoon': 'Course details will be included in the admissions guide.',
      'home.applicationReceived': 'Application interest received. Admissions will be in touch soon.',
      'common.loading': 'Loading…',
      'common.error': 'Something went wrong',
      'common.close': 'Close',
      'common.retry': 'Retry',
      'common.empty': 'No data',
      'common.required': 'Required'
    }
  };

  // Homepage copy is intentionally kept close to the HTML so the Chinese
  // version remains the source of truth; English values live here for the
  // shared toggle used by every page.
  const homeEnglish = {
    'home.metaTitle': 'CGU | China Genshin University',
    'home.metaDescription': 'China Genshin University: discover your academic path across Teyvat.',
    'home.skip': 'Skip to main content',
    'home.utilityBrand': 'CGU · CHINA GENSHIN UNIVERSITY',
    'home.applicationOpen': 'Autumn 2026 applications are open',
    'home.contactAdmissions': 'Contact admissions',
    'home.brandAria': 'CGU China Genshin University home',
    'home.brandBackAria': 'Back to CGU home',
    'home.navAria': 'Main navigation',
    'home.mobileNavAria': 'Mobile navigation',
    'home.navAbout': 'About the university',
    'home.navPrograms': 'Schools & programmes',
    'home.navLife': 'Campus life',
    'home.navAdmissions': 'Admissions',
    'home.heroImageAlt': 'Misty mountain valley beneath a wide sky',
    'home.heroKicker': 'CGU UNIVERSITY · EST. 2026',
    'home.heroTitleLead': 'In Teyvat,',
    'home.heroTitleEm': 'become who you are meant to be',
    'home.heroLede': 'A university for travelers. Turn elemental energy into method, and every encounter into part of your academic journey.',
    'home.explorePrograms': 'Explore schools',
    'home.viewAdmissions': 'Read the admissions guide',
    'home.campusNoteAria': 'Campus note',
    'home.campusNote': 'CAMPUS NOTE 01',
    'home.campusQuote': '“The wind knows the answer.”',
    'home.campusLocation': 'Mondstadt campus · Windrise Court',
    'home.viewCalendar': 'Browse the campus calendar',
    'home.statSchools': 'themed schools',
    'home.statCourses': 'exploration courses',
    'home.statJourneys': 'possible journeys',
    'home.statSchoolsValue': '07',
    'home.statCoursesValue': '42',
    'home.statJourneysValue': '∞',
    'home.scroll': 'Scroll to explore',
    'home.aboutLabel': 'ABOUT CGU',
    'home.aboutKicker': 'THE UNIVERSITY OF POSSIBILITY',
    'home.aboutTitleLead': 'Turn your passion,',
    'home.aboutTitleEm': 'into a field of study.',
    'home.aboutImageAlt': 'A sunlit forest path',
    'home.aboutCaption': 'Windrise · field study route A-01',
    'home.aboutBodyOne': 'Inspired by the seven nations of Teyvat, CGU brings together natural sciences, the humanities, and field practice. Education is not a map; it is the ability to read the direction of the wind.',
    'home.aboutBodyTwo': 'Here, a lesson can happen in a library, a harbor, or on a journey that has not yet begun.',
    'home.aboutLink': 'Discover our teaching philosophy',
    'home.programsKicker': 'FIND YOUR ELEMENT',
    'home.programsTitle': 'Schools & programmes',
    'home.programsAsideLead': 'Begin with the first star,',
    'home.programsAsideTail': 'then choose your direction.',
    'home.programFilterAria': 'Filter programmes by region',
    'home.filterAll': 'All programmes',
    'home.filterMondstadt': 'Mondstadt',
    'home.filterLiyue': 'Liyue',
    'home.filterInazuma': 'Inazuma',
    'home.filterSumeru': 'Sumeru',
    'home.programWindImageAlt': 'Mountains in the morning mist',
    'home.regionMondstadt': 'Mondstadt campus',
    'home.programWindTitle': 'Wind & Natural Sciences',
    'home.programWindDescription': 'Study wind fields, ecosystems, and the edge of free will.',
    'home.degreeScience': 'BSc',
    'home.durationFour': '4 years',
    'home.programContractImageAlt': 'Historic architecture in warm daylight',
    'home.regionLiyue': 'Liyue campus',
    'home.programContractTitle': 'Contracts & Commerce',
    'home.programContractDescription': 'From the harbor, understand order, exchange, and trust.',
    'home.degreeBusiness': 'BCom',
    'home.programDesignImageAlt': 'A quiet coast under night light',
    'home.regionInazuma': 'Inazuma campus',
    'home.programDesignTitle': 'Eternity & Design Practice',
    'home.programDesignDescription': 'Find stability in change and create within constraints.',
    'home.degreeDesign': 'BA Design',
    'home.programWisdomImageAlt': 'A still valley beneath the stars',
    'home.regionSumeru': 'Sumeru campus',
    'home.programWisdomTitle': 'Wisdom & Life Studies',
    'home.programWisdomDescription': 'Bring knowledge out of the terminal and back to living systems.',
    'home.degreeLifeScience': 'MSc',
    'home.durationTwo': '2 years',
    'home.viewProgram': 'View programme',
    'home.programsMore': '38 more interdisciplinary courses',
    'home.downloadCatalog': 'Get the full course catalogue',
    'home.lifeKicker': 'BEYOND THE CLASSROOM',
    'home.lifeTitle': 'Campus life',
    'home.viewAllNews': 'View all campus news',
    'home.featureImageAlt': 'Open mountain landscape at sunset',
    'home.featureTag': 'FEATURED EVENT',
    'home.featureTitle': 'Autumn Equinox: set out for the stars',
    'home.featureDescription': 'The first cross-campus field experience of the new term, from Windrise to the Chasm.',
    'home.registerEvent': 'Register for the event',
    'home.newsListAria': 'Campus news list',
    'home.newsAdmissionsAria': 'Read the autumn admissions notice',
    'home.newsCampusAria': 'Read the campus facilities notice',
    'home.newsResearchAria': 'Read the research call',
    'home.newsAdmissionsType': 'ADMISSIONS',
    'home.newsAdmissionsTitle': 'The 2026 autumn admissions guide is live',
    'home.newsAdmissionsDescription': 'Undergraduate and graduate applications are now open.',
    'home.newsCampusType': 'CAMPUS',
    'home.newsCampusTitle': 'Windrise Court opens for the new term',
    'home.newsCampusDescription': 'The new library and evening study hall are ready for students.',
    'home.newsResearchType': 'RESEARCH',
    'home.newsResearchTitle': 'Element & City research team launches',
    'home.newsResearchDescription': 'A cross-school project on cities, energy, and culture is recruiting.',
    'home.admissionsKicker': 'YOUR NEXT CHAPTER',
    'home.admissionsTitle': 'Ready to set out?',
    'home.admissionsDescription': 'Autumn 2026 applications are open. Send CGU a letter with your curiosity.',
    'home.startApplication': 'Start an application',
    'home.footerTagline': 'A classroom as wide as the world.',
    'home.footerMottoLead': 'Make the world your classroom,',
    'home.footerMottoTail': 'and passion your direction.',
    'home.footerExplore': 'Explore CGU',
    'home.footerAbout': 'About us',
    'home.footerSupport': 'Apply & support',
    'home.footerAdmissionsOffice': 'Admissions office',
    'home.footerEmail': 'hello@cgu-university.example',
    'home.footerAddress': 'Liyue Harbor · Yujing Terrace 7',
    'home.footerHours': 'Mon–Fri 09:00–18:00',
    'home.dialogKicker': 'CGU ADMISSIONS · 2026',
    'home.dialogTitle': 'Start with a letter.',
    'home.dialogIntro': 'Leave your details and an admissions adviser will send you the application guide within three working days.',
    'home.formName': 'Your name',
    'home.formEmail': 'Email address',
    'home.formSchool': 'School of interest',
    'home.schoolUndecided': 'Still exploring',
    'home.formNamePlaceholder': 'Traveler name',
    'home.formEmailPlaceholder': 'name@example.com',
    'home.submitApplication': 'Send enquiry',
    'home.formNote': 'By submitting, you agree that CGU may contact you.',
    'home.programDetailsSoon': 'Course details will be included in the admissions guide.',
    'home.applicationReceived': 'Application interest received. Admissions will be in touch soon.'
  };

  // Keep the shared fallback aligned with the homepage's current seven-nation copy.
  Object.assign(homeEnglish, {
    'home.applicationOpen': 'Autumn 2026 applications open · Snezhnaya track added',
    'home.login': 'Sign in',
    'home.apply': 'Apply now',
    'home.closeApplication': 'Close application window',
    'home.footerLogin': 'Sign in',
    'home.campusQuote': '“The snowfields of Snezhnaya are waiting for new researchers.”',
    'home.campusLocation': 'Snezhnaya campus · Polar research frontier',
    'home.filterFontaine': 'Fontaine',
    'home.filterNatlan': 'Natlan',
    'home.filterSnezhnaya': 'Snezhnaya',
    'home.regionFontaine': 'Fontaine campus',
    'home.regionNatlan': 'Natlan campus',
    'home.regionSnezhnaya': 'Snezhnaya campus',
    'home.programJusticeImageAlt': 'Bright architecture beside the water',
    'home.programFlameImageAlt': 'Open landscape in strong sunlight',
    'home.programPolarImageAlt': 'Snow mountains beneath a polar night sky',
    'home.programJusticeTitle': 'Judgment and mechanical civilization',
    'home.programFlameTitle': 'Fire and competitive ecology',
    'home.programPolarTitle': 'Snezhnaya studies and polar governance',
    'home.programJusticeDescription': 'Study rules, energy, and invention through Fontaine’s courts and workshops.',
    'home.programFlameDescription': 'Conduct field research between tribes, rituals, and the arena.',
    'home.programPolarDescription': 'Use Version 7.0 “Everwinter Without Mercy” as a starting point for polar society and travel ethics.',
    'home.aboutSectionNumber': '01',
    'home.programWindNumber': '01',
    'home.programContractNumber': '02',
    'home.programDesignNumber': '03',
    'home.programWisdomNumber': '04',
    'home.programJusticeNumber': '05',
    'home.programFlameNumber': '06',
    'home.programPolarNumber': '07',
    'home.degreeEngineering': 'BEng',
    'home.degreeFieldwork': 'BA Fieldwork',
    'home.degreePolarStudies': 'MSc Polar Studies',
    'home.programsMore': 'Interdisciplinary courses across all seven nations',
    'home.viewAllNews': 'Read official Genshin news',
    'home.featureTitle': 'Snezhnaya opening week: begin a new research journey in 7.0',
    'home.featureDescription': 'The official Version 7.0 “Everwinter Without Mercy” brings the journey to Snezhnaya; CGU now offers a matching polar studies track.',
    'home.featureDate': '08.12',
    'home.featureYear': '2026',
    'home.registerEvent': 'Read the official version notice',
    'home.newsSnezhnayaType': 'WORLD UPDATE',
    'home.newsSnezhnayaTitle': 'Version 7.0 “Everwinter Without Mercy”: Snezhnaya campus opens',
    'home.newsSnezhnayaDescription': 'Following the official update, Snezhnaya becomes the next stage of the journey and CGU opens a related research track.',
    'home.newsSnezhnayaAria': 'Read the official Snezhnaya 7.0 news',
    'home.newsSnezhnayaDate': '08.12',
    'home.newsSnezhnayaYear': '2026',
    'home.newsCampusDate': '08.05',
    'home.newsCampusYear': '2026',
    'home.newsResearchDate': '07.24',
    'home.newsResearchYear': '2026'
  });

  const defaultText = new WeakMap();
  const defaultAttrs = new WeakMap();
  const catalogDefaults = new Map();
  const managedAssetDefaults = {
    'asset.heroImage': 'https://images.unsplash.com/photo-1500534623283-312aade485b7?auto=format&fit=crop&w=1800&q=85',
    'asset.aboutImage': 'https://images.unsplash.com/photo-1511497584788-876760111969?auto=format&fit=crop&w=1000&q=85',
    'asset.featureImage': 'https://images.unsplash.com/photo-1500534314209-a25ddb2bd429?auto=format&fit=crop&w=1400&q=85',
    'asset.programWindImage': 'https://images.unsplash.com/photo-1500534623283-312aade485b7?auto=format&fit=crop&w=900&q=80',
    'asset.programContractImage': 'https://images.unsplash.com/photo-1548013146-72479768bada?auto=format&fit=crop&w=900&q=80',
    'asset.programDesignImage': 'https://images.unsplash.com/photo-1518709268805-4e9042af9f23?auto=format&fit=crop&w=900&q=80',
    'asset.programWisdomImage': 'https://images.unsplash.com/photo-1534447677768-be436bb09401?auto=format&fit=crop&w=900&q=80',
    'asset.programJusticeImage': 'https://images.unsplash.com/photo-1494526585095-c41746248156?auto=format&fit=crop&w=900&q=80',
    'asset.programFlameImage': 'https://images.unsplash.com/photo-1500534314209-a25ddb2bd429?auto=format&fit=crop&w=900&q=80',
    'asset.programPolarImage': 'https://images.unsplash.com/photo-1519681393784-d120267933ba?auto=format&fit=crop&w=900&q=80',
    'link.officialNews': 'https://genshin.hoyoverse.com/zh-tw/news',
    'link.featureNews': 'https://genshin.hoyoverse.com/zh-tw/news',
    'link.newsSnezhnaya': 'https://genshin.hoyoverse.com/zh-tw/news',
    'link.newsCampus': '#contact',
    'link.newsResearch': '#programs',
    'link.footerEmail': 'mailto:hello@cgu-university.example'
  };

  const getInitialLocale = () => {
    const saved = window.localStorage?.getItem(STORAGE_KEY);
    if (saved === 'zh' || saved === 'en') return saved;
    const languages = Array.isArray(navigator.languages) && navigator.languages.length
      ? navigator.languages
      : [navigator.language];
    return languages.some((language) => /^zh(?:-|$)/i.test(language || '')) ? 'zh' : 'en';
  };

  let locale = getInitialLocale();

  const mergeSiteContent = (items) => {
    (Array.isArray(items) ? items : []).forEach((item) => {
      const key = String(item?.key || '').trim();
      if (!key) return;
      if (typeof item.zh === 'string' && item.zh.trim()) dictionaries.zh[key] = item.zh;
      if (typeof item.en === 'string' && item.en.trim()) dictionaries.en[key] = item.en;
    });
  };

  const contentReady = fetch('/api/site-content', { credentials: 'same-origin', headers: { Accept: 'application/json' } })
    .then((response) => response.ok ? response.json() : null)
    .then((payload) => mergeSiteContent(payload?.content || payload?.data?.content || []))
    .catch(() => { /* Static defaults remain available when the API is offline. */ });

  const translate = (key, vars = {}) => {
    const value = dictionaries[locale]?.[key] ?? (locale === 'en' ? homeEnglish[key] : undefined) ?? dictionaries.zh[key] ?? key;
    return String(value).replace(/\{(\w+)\}/g, (_, name) => vars[name] ?? '');
  };

  const apply = (root = document) => {
    root.querySelectorAll?.('[data-i18n]').forEach((node) => {
      const key = node.dataset.i18n;
      if (!defaultText.has(node)) {
        defaultText.set(node, node.textContent);
        if (key && !catalogDefaults.has(key)) catalogDefaults.set(key, { zh: node.textContent.trim() });
      }
      node.textContent = locale === 'zh' && !dictionaries.zh[key] ? defaultText.get(node) : translate(key);
    });
    root.querySelectorAll?.('[data-i18n-attr]').forEach((node) => {
      if (!defaultAttrs.has(node)) {
        const values = {};
        node.dataset.i18nAttr.split(';').forEach((pair) => {
          const [attribute] = pair.split(':').map((part) => part.trim());
          if (attribute) values[attribute] = node.getAttribute(attribute) || '';
        });
        defaultAttrs.set(node, values);
        const keyValues = {};
        node.dataset.i18nAttr.split(';').forEach((pair) => {
          const [attribute, key] = pair.split(':').map((part) => part.trim());
          if (attribute && key && !catalogDefaults.has(key)) {
            catalogDefaults.set(key, { zh: values[attribute] || '' });
          }
          if (attribute && key) keyValues[key] = values[attribute] || '';
        });
      }
      const pairs = node.dataset.i18nAttr.split(';').map((pair) => pair.trim()).filter(Boolean);
      pairs.forEach((pair) => {
        const [attribute, key] = pair.split(':').map((part) => part.trim());
        if (attribute && key) node.setAttribute(attribute, locale === 'zh' && !dictionaries.zh[key] ? defaultAttrs.get(node)[attribute] : translate(key));
      });
    });
    root.querySelectorAll?.('[data-locale-toggle]').forEach((button) => {
      button.textContent = translate('nav.language');
      button.setAttribute('aria-label', translate('nav.language'));
    });
    document.documentElement.lang = locale === 'zh' ? 'zh-CN' : 'en';
    document.documentElement.dataset.locale = locale;
  };

  const collectMarkupCatalog = (root) => {
    root.querySelectorAll?.('[data-i18n]').forEach((node) => {
      const key = String(node.dataset.i18n || '').trim();
      if (key && !catalogDefaults.has(key)) catalogDefaults.set(key, { zh: String(node.textContent || '').trim() });
    });
    root.querySelectorAll?.('[data-i18n-attr]').forEach((node) => {
      String(node.dataset.i18nAttr || '').split(';').forEach((pair) => {
        const [attribute, key] = pair.split(':').map((part) => part.trim());
        if (attribute && key && !catalogDefaults.has(key)) catalogDefaults.set(key, { zh: node.getAttribute(attribute) || '' });
      });
    });
  };

  const collectHomeCatalog = async () => {
    if (document.body?.dataset.page === 'home') return;
    try {
      const response = await fetch('/index.html', { credentials: 'same-origin', headers: { Accept: 'text/html' } });
      if (response.ok) collectMarkupCatalog(new DOMParser().parseFromString(await response.text(), 'text/html'));
    } catch { /* Current page content remains available. */ }
  };

  const catalog = () => {
    const result = new Map();
    catalogDefaults.forEach((value, key) => result.set(key, { key, zh: value.zh || '', en: '' }));
    Object.keys(dictionaries.zh).forEach((key) => result.set(key, { ...(result.get(key) || { key, zh: '', en: '' }), zh: dictionaries.zh[key] }));
    Object.keys(dictionaries.en).forEach((key) => result.set(key, { ...(result.get(key) || { key, zh: '', en: '' }), en: dictionaries.en[key] }));
    Object.keys(homeEnglish).forEach((key) => result.set(key, { ...(result.get(key) || { key, zh: '', en: '' }), en: homeEnglish[key] }));
    Object.entries(managedAssetDefaults).forEach(([key, value]) => result.set(key, { ...(result.get(key) || { key, zh: '', en: '' }), zh: value, en: value }));
    return [...result.values()].filter((item) => item.zh || item.en).sort((a, b) => a.key.localeCompare(b.key));
  };

  const catalogReady = contentReady.then(() => collectHomeCatalog());

  const setLocale = (next) => {
    locale = next === 'en' ? 'en' : 'zh';
    window.localStorage?.setItem(STORAGE_KEY, locale);
    apply();
    window.dispatchEvent(new CustomEvent('cgu:localechange', { detail: { locale } }));
  };

  const pick = (value, fallback = '') => {
    if (value == null) return fallback;
    if (typeof value === 'string') return value;
    if (locale === 'en') return value.en ?? value.nameEn ?? value.titleEn ?? value.zh ?? value.nameZh ?? value.titleZh ?? fallback;
    return value.zh ?? value.nameZh ?? value.titleZh ?? value.en ?? value.nameEn ?? value.titleEn ?? fallback;
  };

  window.CGU_I18N = {
    get locale() { return locale; },
    t: translate,
    apply,
    setLocale,
    pick,
    dictionaries,
    mergeSiteContent,
    ready: catalogReady,
    catalog
  };

  document.addEventListener('DOMContentLoaded', async () => {
    collectMarkupCatalog(document);
    await catalogReady;
    apply();
    document.querySelectorAll('[data-locale-toggle]').forEach((button) => {
      button.addEventListener('click', () => setLocale(locale === 'zh' ? 'en' : 'zh'));
    });
  });
})();
