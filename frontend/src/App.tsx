import { startTransition, useEffect, useRef, useState } from 'react';
import { Footer } from './components/Footer';
import { Navbar } from './components/Navbar';
import {
  APIError,
  authInvalidEventName,
  getAuthLoginURL,
  getCurrentSession,
  listPublicDomains,
  logout,
} from './lib/api';
import { Domains } from './pages/Domains';
import { Emails } from './pages/Emails';
import { Home } from './pages/Home';
import { Login } from './pages/Login';
import { PaymentCallback } from './pages/PaymentCallback';
import { Permissions } from './pages/Permissions';
import { Settings } from './pages/Settings';
import { Supervision } from './pages/Supervision';
import type { Allocation, ManagedDomain, MeResponse, User } from './types/api';

// TabKey enumerates every client-side page supported by the public frontend.
type TabKey = 'home' | 'domains' | 'emails' | 'settings' | 'permissions' | 'supervision' | 'login' | 'payment_callback';

// The anime background preference is browser-local so each visitor can decide
// whether they want the third-party image layer without affecting anyone else.
const animeBackgroundStorageKey = 'linuxdospace.preferences.anime-background-enabled';
const animeBackgroundImageURL = 'https://www.loliapi.com/acg/';

// SessionState mirrors the normalized `/v1/me` payload consumed across pages.
interface SessionState {
  authenticated: boolean;
  oauthConfigured: boolean;
  user?: User;
  csrfToken?: string;
  sessionExpiresAt?: string;
  allocations: Allocation[];
}

// tabPathMap keeps URL updates centralized without bringing in a full router.
const tabPathMap: Record<TabKey, string> = {
  home: '/',
  domains: '/domains',
  emails: '/emails',
  settings: '/settings',
  permissions: '/permissions',
  supervision: '/supervision',
  login: '/login',
  payment_callback: '/payments/callback',
};

// pathToTab converts the current pathname into the matching view key.
function pathToTab(pathname: string): TabKey {
  switch (pathname.toLowerCase()) {
    case '/domains':
      return 'domains';
    case '/emails':
      return 'emails';
    case '/settings':
      return 'settings';
    case '/permissions':
      return 'permissions';
    case '/supervision':
      return 'supervision';
    case '/login':
      return 'login';
    case '/payments/callback':
      return 'payment_callback';
    default:
      return 'home';
  }
}

// normalizeSessionResponse converts the backend response into the smaller shape
// shared by the public-site pages.
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

// readAnimeBackgroundPreference restores the persisted browser preference. The
// default intentionally stays enabled so the previous anime-style design remains
// the out-of-box experience unless the visitor turns it off.
function readAnimeBackgroundPreference(): boolean {
  if (typeof window === 'undefined') {
    return true;
  }

  try {
    const storedPreference = window.localStorage.getItem(animeBackgroundStorageKey);
    if (storedPreference === null) {
      return true;
    }

    return storedPreference === 'true';
  } catch {
    return true;
  }
}

interface SiteBackgroundProps {
  animeBackgroundEnabled: boolean;
}

// SiteBackground keeps the current local gradient composition in all cases.
// When anime mode is enabled, the remote image sits underneath the local layers
// so the site still looks like the current design instead of reverting to a raw photo background.
function SiteBackground({ animeBackgroundEnabled }: SiteBackgroundProps) {
  return (
    <>
      {animeBackgroundEnabled && (
        <div
          className="fixed inset-0 z-[-4] bg-cover bg-center bg-no-repeat transition-opacity duration-700 dark:brightness-[0.3]"
          style={{ backgroundImage: `url(${animeBackgroundImageURL})` }}
        />
      )}
      <div
        className={`fixed inset-0 z-[-3] bg-[radial-gradient(circle_at_top_left,_rgba(250,204,21,0.26),_transparent_38%),radial-gradient(circle_at_top_right,_rgba(56,189,248,0.18),_transparent_34%),linear-gradient(160deg,_#f8fafc_0%,_#e2e8f0_46%,_#cbd5e1_100%)] transition-all duration-500 dark:bg-[radial-gradient(circle_at_top_left,_rgba(234,179,8,0.16),_transparent_38%),radial-gradient(circle_at_top_right,_rgba(34,211,238,0.12),_transparent_34%),linear-gradient(160deg,_#020617_0%,_#0f172a_54%,_#111827_100%)] ${
          animeBackgroundEnabled ? 'opacity-70' : 'opacity-100'
        }`}
      />
      <div className="fixed inset-0 z-[-2] overflow-hidden">
        <div className="absolute -left-20 top-12 h-64 w-64 rounded-full bg-amber-300/35 blur-3xl dark:bg-amber-500/18" />
        <div className="absolute right-[-5rem] top-1/4 h-72 w-72 rounded-full bg-sky-300/30 blur-3xl dark:bg-cyan-400/16" />
        <div className="absolute bottom-[-6rem] left-1/3 h-80 w-80 rounded-full bg-emerald-300/18 blur-3xl dark:bg-emerald-400/12" />
      </div>
      <div className="fixed inset-0 z-[-1] bg-white/40 backdrop-blur-[2px] transition-colors duration-500 dark:bg-black/40" />
    </>
  );
}

// readableErrorMessage turns browser-side failures into one stable user-facing message.
function readableErrorMessage(error: unknown, fallback: string): string {
  if (error instanceof APIError) {
    return error.message;
  }
  if (error instanceof Error && error.message.trim() !== '') {
    return error.message;
  }
  return fallback;
}

// authErrorMessage converts OAuth callback error codes into public-site copy.
function authErrorMessage(raw: string | null): string {
  switch ((raw || '').trim().toLowerCase()) {
    case 'forbidden':
      return '当前账号暂时无法登录 LinuxDoSpace。';
    case 'service_unavailable':
      return 'Linux Do 登录暂时不可用，请稍后重试。';
    case 'unauthorized':
      return '登录状态校验失败，请重新发起登录。';
    case 'validation_error':
      return '登录回调参数不完整，请重新发起登录。';
    default:
      return raw ? `登录失败：${raw}` : '';
  }
}

// App coordinates page switching, session refresh, and the shared public layout.
export default function App() {
  const [activeTab, setActiveTab] = useState<TabKey>(() => pathToTab(window.location.pathname));
  const [isDark, setIsDark] = useState(false);
  const [animeBackgroundEnabled, setAnimeBackgroundEnabled] = useState<boolean>(() => readAnimeBackgroundPreference());
  const [authBanner, setAuthBanner] = useState(() => authErrorMessage(new URLSearchParams(window.location.search).get('auth_error')));
  const [session, setSession] = useState<SessionState>({
    authenticated: false,
    oauthConfigured: false,
    allocations: [],
  });
  const [sessionLoading, setSessionLoading] = useState(true);
  const [sessionError, setSessionError] = useState('');
  const [publicDomains, setPublicDomains] = useState<ManagedDomain[]>([]);
  const [domainsLoading, setDomainsLoading] = useState(true);
  const [domainsError, setDomainsError] = useState('');
  const sessionRequestTokenRef = useRef(0);
  const domainsRequestTokenRef = useRef(0);

  useEffect(() => {
    if (isDark) {
      document.documentElement.classList.add('dark');
      return;
    }
    document.documentElement.classList.remove('dark');
  }, [isDark]);

  useEffect(() => {
    try {
      window.localStorage.setItem(animeBackgroundStorageKey, String(animeBackgroundEnabled));
    } catch {
      // Ignore storage write failures and keep the current in-memory choice.
    }
  }, [animeBackgroundEnabled]);

  useEffect(() => {
    const search = new URLSearchParams(window.location.search);
    if (search.has('auth_error')) {
      search.delete('auth_error');
      const nextSearch = search.toString();
      const nextURL = `${window.location.pathname}${nextSearch ? `?${nextSearch}` : ''}${window.location.hash}`;
      window.history.replaceState(null, '', nextURL);
    }
  }, []);

  useEffect(() => {
    const handleStorage = (event: StorageEvent) => {
      if (event.key !== animeBackgroundStorageKey) {
        return;
      }

      setAnimeBackgroundEnabled(event.newValue !== 'false');
    };

    window.addEventListener('storage', handleStorage);
    return () => window.removeEventListener('storage', handleStorage);
  }, []);

  useEffect(() => {
    const refreshOnFocus = () => {
      if (document.visibilityState === 'hidden') {
        return;
      }
      void refreshSession({ silent: true });
    };

    window.addEventListener('focus', refreshOnFocus);
    document.addEventListener('visibilitychange', refreshOnFocus);
    return () => {
      window.removeEventListener('focus', refreshOnFocus);
      document.removeEventListener('visibilitychange', refreshOnFocus);
    };
  }, []);

  useEffect(() => {
    if (!session.authenticated) {
      return;
    }

    const refreshInterval = window.setInterval(() => {
      void refreshSession({ silent: true });
    }, 2 * 60 * 1000);

    return () => window.clearInterval(refreshInterval);
  }, [session.authenticated]);

  useEffect(() => {
    const handleAuthInvalid = () => {
      sessionRequestTokenRef.current += 1;
      setSession((current) => {
        if (!current.authenticated) {
          return current;
        }
        return {
          authenticated: false,
          oauthConfigured: current.oauthConfigured,
          allocations: [],
        };
      });
      setSessionError('登录状态已失效，请重新登录。');
    };

    window.addEventListener(authInvalidEventName, handleAuthInvalid);
    return () => window.removeEventListener(authInvalidEventName, handleAuthInvalid);
  }, []);

  useEffect(() => {
    const handlePopState = () => {
      startTransition(() => {
        setActiveTab(pathToTab(window.location.pathname));
      });
    };

    window.addEventListener('popstate', handlePopState);
    return () => window.removeEventListener('popstate', handlePopState);
  }, []);

  useEffect(() => {
    const expectedPath = tabPathMap[activeTab];
    if (window.location.pathname !== expectedPath) {
      window.history.pushState({}, '', expectedPath);
    }
  }, [activeTab]);

  useEffect(() => {
    void refreshSession();
    void refreshPublicDomains();
  }, []);

  async function refreshSession(options: { silent?: boolean } = {}): Promise<void> {
    const { silent = false } = options;
    const requestToken = ++sessionRequestTokenRef.current;
    if (!silent) {
      setSessionLoading(true);
    }

    try {
      const response = await getCurrentSession();
      if (requestToken !== sessionRequestTokenRef.current) {
        return;
      }
      setSession(normalizeSessionResponse(response));
      if (response.authenticated) {
        setAuthBanner('');
      }
      setSessionError('');
    } catch (error) {
      if (requestToken !== sessionRequestTokenRef.current) {
        return;
      }
      const shouldClearSession = error instanceof APIError && (error.code === 'unauthorized' || error.code === 'forbidden');
      setSession((current) => {
        if (shouldClearSession) {
          return {
            authenticated: false,
            oauthConfigured: current.oauthConfigured,
            allocations: [],
          };
        }
        if (current.authenticated) {
          return current;
        }
        return {
          authenticated: false,
          oauthConfigured: current.oauthConfigured,
          allocations: [],
        };
      });
      setSessionError(readableErrorMessage(error, '无法加载当前登录状态'));
    } finally {
      if (!silent && requestToken === sessionRequestTokenRef.current) {
        setSessionLoading(false);
      }
    }
  }

  async function refreshPublicDomains(): Promise<void> {
    const requestToken = ++domainsRequestTokenRef.current;
    setDomainsLoading(true);

    try {
      const domains = await listPublicDomains();
      if (requestToken !== domainsRequestTokenRef.current) {
        return;
      }
      setPublicDomains(domains);
      setDomainsError('');
    } catch (error) {
      if (requestToken !== domainsRequestTokenRef.current) {
        return;
      }
      setPublicDomains([]);
      setDomainsError(readableErrorMessage(error, '无法加载可分发域名列表'));
    } finally {
      if (requestToken === domainsRequestTokenRef.current) {
        setDomainsLoading(false);
      }
    }
  }

  function navigateToTab(tab: TabKey): void {
    startTransition(() => {
      setActiveTab(tab);
    });
  }

  function beginLogin(nextTab: TabKey): void {
    setAuthBanner('');
    window.location.assign(getAuthLoginURL(tabPathMap[nextTab]));
  }

  function beginLoginToPath(nextPath: string): void {
    setAuthBanner('');
    window.location.assign(getAuthLoginURL(nextPath));
  }

  async function handleLogout(): Promise<void> {
    if (!session.csrfToken) {
      return;
    }

    sessionRequestTokenRef.current += 1;
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

  async function handleAllocationCreated(): Promise<void> {
    await refreshSession({ silent: true });
    navigateToTab('settings');
  }

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
      case 'emails':
        return (
          <Emails
            authenticated={session.authenticated}
            sessionLoading={sessionLoading}
            user={session.user}
            publicDomains={publicDomains}
            csrfToken={session.csrfToken}
            onLogin={() => beginLogin('emails')}
          />
        );
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
      case 'permissions':
        return (
          <Permissions
            authenticated={session.authenticated}
            sessionLoading={sessionLoading}
            user={session.user}
            csrfToken={session.csrfToken}
            onLogin={() => beginLogin('permissions')}
            onOpenEmails={() => navigateToTab('emails')}
          />
        );
      case 'payment_callback':
        return (
          <PaymentCallback
            authenticated={session.authenticated}
            sessionLoading={sessionLoading}
            user={session.user}
            csrfToken={session.csrfToken}
            onLogin={() => beginLoginToPath(`${window.location.pathname}${window.location.search}`)}
            onOpenPermissions={() => navigateToTab('permissions')}
          />
        );
      case 'supervision':
        return <Supervision />;
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

  const bannerMessage = authBanner || sessionError || domainsError;

  return (
    <div className="relative min-h-screen overflow-x-hidden font-sans text-gray-900 transition-colors duration-500 dark:text-white">
      <SiteBackground animeBackgroundEnabled={animeBackgroundEnabled} />

      <Navbar
        activeTab={activeTab}
        setActiveTab={navigateToTab}
        isDark={isDark}
        toggleTheme={() => setIsDark((currentValue) => !currentValue)}
        animeBackgroundEnabled={animeBackgroundEnabled}
        onAnimeBackgroundEnabledChange={setAnimeBackgroundEnabled}
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

      {bannerMessage && (
        <div className="relative z-20 px-6 pt-24">
          <div className="mx-auto max-w-5xl rounded-2xl border border-amber-300/40 bg-amber-100/70 px-4 py-3 text-sm text-amber-900 shadow-lg backdrop-blur-md dark:border-amber-700/40 dark:bg-amber-950/40 dark:text-amber-200">
            {bannerMessage}
          </div>
        </div>
      )}

      <main className="relative z-10 min-h-screen">{renderContent()}</main>

      <Footer />
    </div>
  );
}
