import { startTransition, useEffect, useState } from 'react';
import { Navbar } from './components/Navbar';
import { Footer } from './components/Footer';
import { Home } from './pages/Home';
import { Domains } from './pages/Domains';
import { Supervision } from './pages/Supervision';
import { Settings } from './pages/Settings';
import { Login } from './pages/Login';
import {
  APIError,
  getAuthLoginURL,
  getCurrentSession,
  listPublicDomains,
  logout,
} from './lib/api';
import type { Allocation, ManagedDomain, MeResponse, User } from './types/api';

// TabKey 定义了前端当前支持的五个主视图。
// 由于本项目暂时没有接入 React Router，所以我们手工维护 tab 与 URL 的双向映射。
type TabKey = 'home' | 'domains' | 'supervision' | 'settings' | 'login';

// SessionState 表示前端缓存的当前登录态。
// 该状态来自后端 `/v1/me`，并被多个页面共同消费。
interface SessionState {
  authenticated: boolean;
  oauthConfigured: boolean;
  user?: User;
  csrfToken?: string;
  sessionExpiresAt?: string;
  allocations: Allocation[];
}

// tabPathMap 把前端视图与浏览器 URL 关联起来。
// 这样做的主要目的，是让后端 OAuth 回调重定向到 `/settings` 时，前端能正确打开配置页。
const tabPathMap: Record<TabKey, string> = {
  home: '/',
  domains: '/domains',
  supervision: '/supervision',
  settings: '/settings',
  login: '/login',
};

// pathToTab 根据当前浏览器路径推导要显示的主视图。
function pathToTab(pathname: string): TabKey {
  switch (pathname.toLowerCase()) {
    case '/domains':
      return 'domains';
    case '/supervision':
      return 'supervision';
    case '/settings':
      return 'settings';
    case '/login':
      return 'login';
    default:
      return 'home';
  }
}

// normalizeSessionResponse 把后端 `/v1/me` 的返回结果规范化为前端统一状态。
function normalizeSessionResponse(response: MeResponse): SessionState {
  return {
    authenticated: response.authenticated,
    oauthConfigured: response.oauth_configured ?? response.authenticated,
    user: response.user,
    csrfToken: response.csrf_token,
    sessionExpiresAt: response.session_expires_at,
    allocations: response.allocations ?? [],
  };
}

export default function App() {
  // activeTab 保存当前主视图。
  const [activeTab, setActiveTab] = useState<TabKey>(() => pathToTab(window.location.pathname));

  // isDark 控制深色模式切换，沿用现有 UI 表现。
  const [isDark, setIsDark] = useState(false);

  // session 保存当前登录态与用户已持有的命名空间分配。
  const [session, setSession] = useState<SessionState>({
    authenticated: false,
    oauthConfigured: false,
    allocations: [],
  });

  // sessionLoading 用于控制首次加载与手动刷新时的状态提示。
  const [sessionLoading, setSessionLoading] = useState(true);

  // sessionError 用于存放登录态刷新失败的错误信息。
  const [sessionError, setSessionError] = useState('');

  // publicDomains 保存公开可申请的根域名列表。
  const [publicDomains, setPublicDomains] = useState<ManagedDomain[]>([]);

  // domainsLoading 用于控制根域名列表的加载态。
  const [domainsLoading, setDomainsLoading] = useState(true);

  // domainsError 用于存放根域名列表加载失败的错误信息。
  const [domainsError, setDomainsError] = useState('');

  // 主题切换仍然沿用 DOM 根元素上的 `.dark` 类控制，不改现有样式体系。
  useEffect(() => {
    if (isDark) {
      document.documentElement.classList.add('dark');
    } else {
      document.documentElement.classList.remove('dark');
    }
  }, [isDark]);

  // 监听浏览器前进后退操作，并把路径变化同步到当前 tab。
  useEffect(() => {
    const handlePopState = () => {
      startTransition(() => {
        setActiveTab(pathToTab(window.location.pathname));
      });
    };

    window.addEventListener('popstate', handlePopState);
    return () => window.removeEventListener('popstate', handlePopState);
  }, []);

  // 当 tab 变化时，把新的视图状态反映到浏览器地址栏。
  useEffect(() => {
    const expectedPath = tabPathMap[activeTab];
    if (window.location.pathname !== expectedPath) {
      window.history.pushState({}, '', expectedPath);
    }
  }, [activeTab]);

  // 首次进入页面时，拉取登录态和公开根域名列表。
  useEffect(() => {
    void refreshSession();
    void refreshPublicDomains();
  }, []);

  // refreshSession 刷新当前登录态。
  // `silent=true` 时不会打断当前页面，只静默更新。
  async function refreshSession(options: { silent?: boolean } = {}): Promise<void> {
    const { silent = false } = options;
    if (!silent) {
      setSessionLoading(true);
    }

    try {
      const response = await getCurrentSession();
      setSession(normalizeSessionResponse(response));
      setSessionError('');
    } catch (error) {
      setSession({
        authenticated: false,
        oauthConfigured: false,
        allocations: [],
      });
      setSessionError(readableErrorMessage(error, '无法加载当前登录状态'));
    } finally {
      if (!silent) {
        setSessionLoading(false);
      }
    }
  }

  // refreshPublicDomains 刷新公开根域名列表。
  async function refreshPublicDomains(): Promise<void> {
    setDomainsLoading(true);

    try {
      const domains = await listPublicDomains();
      setPublicDomains(domains);
      setDomainsError('');
    } catch (error) {
      setPublicDomains([]);
      setDomainsError(readableErrorMessage(error, '无法加载可分发域名列表'));
    } finally {
      setDomainsLoading(false);
    }
  }

  // navigateToTab 负责统一切换当前主视图。
  function navigateToTab(tab: TabKey): void {
    startTransition(() => {
      setActiveTab(tab);
    });
  }

  // beginLogin 把用户送到后端 OAuth 登录入口。
  function beginLogin(nextTab: TabKey): void {
    window.location.assign(getAuthLoginURL(tabPathMap[nextTab]));
  }

  // handleLogout 请求后端销毁当前会话，然后回到首页。
  async function handleLogout(): Promise<void> {
    if (!session.csrfToken) {
      return;
    }

    try {
      await logout(session.csrfToken);
      setSession({
        authenticated: false,
        oauthConfigured: session.oauthConfigured,
        allocations: [],
      });
      setSessionError('');
      navigateToTab('home');
    } catch (error) {
      setSessionError(readableErrorMessage(error, '退出登录失败'));
    }
  }

  // handleAllocationCreated 在用户成功申请命名空间后刷新会话并跳到配置页。
  async function handleAllocationCreated(): Promise<void> {
    await refreshSession({ silent: true });
    navigateToTab('settings');
  }

  // renderContent 根据当前 tab 渲染对应页面。
  function renderContent() {
    switch (activeTab) {
      case 'home':
        return <Home />;
      case 'domains':
        return (
          <Domains
            publicDomains={publicDomains}
            domainsLoading={domainsLoading}
            domainsError={domainsError}
            authenticated={session.authenticated}
            user={session.user}
            allocations={session.allocations}
            csrfToken={session.csrfToken}
            onLogin={() => beginLogin('domains')}
            onAllocationCreated={handleAllocationCreated}
          />
        );
      case 'supervision':
        return <Supervision />;
      case 'settings':
        return (
          <Settings
            authenticated={session.authenticated}
            sessionLoading={sessionLoading}
            user={session.user}
            allocations={session.allocations}
            csrfToken={session.csrfToken}
            onLogin={() => beginLogin('settings')}
            onNavigateDomains={() => navigateToTab('domains')}
            onSessionRefresh={() => refreshSession({ silent: true })}
            onLogout={handleLogout}
          />
        );
      case 'login':
        return (
          <Login
            authenticated={session.authenticated}
            oauthConfigured={session.oauthConfigured}
            user={session.user}
            onLogin={() => beginLogin('settings')}
            onOpenSettings={() => navigateToTab('settings')}
            onLogout={handleLogout}
          />
        );
      default:
        return <Home />;
    }
  }

  // bannerMessage 用于在页面顶部展示最重要的后端连接错误。
  const bannerMessage = sessionError || domainsError;

  return (
    <div className="min-h-screen font-sans text-gray-900 dark:text-white transition-colors duration-500 overflow-x-hidden relative">
      {/* 背景图继续沿用原有设计，不对视觉方向做结构性修改。 */}
      <div
        className="fixed inset-0 z-[-2] bg-cover bg-center bg-no-repeat transition-all duration-1000 dark:brightness-[0.3]"
        style={{ backgroundImage: 'url(https://www.loliapi.com/acg/)' }}
      />

      {/* 可读性遮罩继续保留，避免背景图干扰文字层级。 */}
      <div className="fixed inset-0 z-[-1] bg-white/40 dark:bg-black/40 backdrop-blur-[2px] transition-colors duration-500" />

      <Navbar
        activeTab={activeTab}
        setActiveTab={navigateToTab}
        isDark={isDark}
        toggleTheme={() => setIsDark(!isDark)}
        authenticated={session.authenticated}
        displayName={session.user?.display_name || session.user?.username}
        onAuthAction={() => {
          if (session.authenticated) {
            navigateToTab('settings');
            return;
          }
          navigateToTab('login');
        }}
      />

      {/* 顶部横幅只在后端连接异常时显示，避免改动主页面布局。 */}
      {bannerMessage && (
        <div className="pt-24 px-6 relative z-20">
          <div className="max-w-5xl mx-auto rounded-2xl border border-amber-300/40 bg-amber-100/70 dark:bg-amber-950/40 dark:border-amber-700/40 backdrop-blur-md px-4 py-3 text-sm text-amber-900 dark:text-amber-200 shadow-lg">
            {bannerMessage}
          </div>
        </div>
      )}

      <main className="relative z-10 min-h-screen">{renderContent()}</main>

      <Footer />
    </div>
  );
}

// readableErrorMessage 把浏览器端的异常统一整理为对用户更友好的文案。
function readableErrorMessage(error: unknown, fallback: string): string {
  if (error instanceof APIError) {
    return error.message;
  }
  if (error instanceof Error && error.message.trim() !== '') {
    return error.message;
  }
  return fallback;
}
