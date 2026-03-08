import { startTransition, useEffect, useMemo, useState } from 'react';
import { ShieldCheck } from 'lucide-react';
import { AdminNavbar } from './components/AdminNavbar';
import { APIError, getAdminLoginURL, getAdminSession, logout } from './lib/api';
import { AdminLogin } from './pages/AdminLogin';
import { ApplicationsPage } from './pages/ApplicationsPage';
import { DomainsPage } from './pages/DomainsPage';
import { EmailsPage } from './pages/EmailsPage';
import { RedeemCodesPage } from './pages/RedeemCodesPage';
import { UsersPage } from './pages/UsersPage';
import type { AdminSessionResponse, AdminTabKey, ManagedDomain } from './types/admin';

const STORAGE_KEYS = {
  theme: 'linuxdospace-admin-theme',
} as const;

function tabFromHash(hash: string): AdminTabKey {
  switch (hash.replace('#', '').toLowerCase()) {
    case 'domains':
      return 'domains';
    case 'emails':
      return 'emails';
    case 'applications':
      return 'applications';
    case 'redeem':
      return 'redeem';
    case 'users':
    default:
      return 'users';
  }
}

function currentAdminNextPath(tab: AdminTabKey): string {
  return `/#${tab}`;
}

function authErrorMessage(raw: string | null): string {
  switch ((raw || '').trim().toLowerCase()) {
    case 'admin_required':
      return '当前 Linux Do 账号没有被授予管理员权限，请切换账号后重试。';
    case 'forbidden':
      return '当前账号已被拒绝访问管理员控制台。';
    case 'unauthorized':
      return '管理员会话已失效，请重新登录。';
    case 'service_unavailable':
      return '后端当前无法完成 Linux Do 登录，请稍后重试。';
    default:
      return raw ? `管理员登录失败：${raw}` : '';
  }
}

export default function App() {
  const [isDark, setIsDark] = useState<boolean>(() => window.localStorage.getItem(STORAGE_KEYS.theme) === 'dark');
  const [activeTab, setActiveTab] = useState<AdminTabKey>(() => tabFromHash(window.location.hash));
  const [session, setSession] = useState<AdminSessionResponse | null>(null);
  const [managedDomains, setManagedDomains] = useState<ManagedDomain[]>([]);
  const [sessionLoading, setSessionLoading] = useState(true);
  const [sessionError, setSessionError] = useState(() => authErrorMessage(new URLSearchParams(window.location.search).get('auth_error')));

  const loginURL = useMemo(() => getAdminLoginURL(currentAdminNextPath(activeTab)), [activeTab]);

  useEffect(() => {
    document.documentElement.classList.toggle('dark', isDark);
    window.localStorage.setItem(STORAGE_KEYS.theme, isDark ? 'dark' : 'light');
  }, [isDark]);

  useEffect(() => {
    const nextHash = `#${activeTab}`;
    if (window.location.hash !== nextHash) {
      window.history.replaceState(null, '', `${window.location.pathname}${window.location.search}${nextHash}`);
    }
  }, [activeTab]);

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
    const handleHashChange = () => {
      startTransition(() => {
        setActiveTab(tabFromHash(window.location.hash));
      });
    };

    window.addEventListener('hashchange', handleHashChange);
    return () => window.removeEventListener('hashchange', handleHashChange);
  }, []);

  async function loadSession() {
    try {
      setSessionLoading(true);
      const data = await getAdminSession();
      setSession(data);
      setManagedDomains(data.managed_domains ?? []);
      if (!data.authorized && data.authenticated) {
        setSessionError('当前账号已登录，但没有管理员权限。');
      } else if (data.authenticated) {
        setSessionError('');
      }
    } catch (error) {
      if (error instanceof APIError) {
        setSessionError(error.message);
      } else {
        setSessionError('无法连接管理员后端。');
      }
    } finally {
      setSessionLoading(false);
    }
  }

  useEffect(() => {
    void loadSession();
  }, []);

  async function handleLogout() {
    if (!session?.csrf_token) {
      setSession(null);
      setManagedDomains([]);
      return;
    }
    try {
      await logout(session.csrf_token);
    } catch {
      // Ignore logout transport errors and force local signed-out state.
    }
    setSession(null);
    setManagedDomains([]);
    setSessionError('');
  }

  function renderContent() {
    if (!session?.csrf_token) {
      return null;
    }

    switch (activeTab) {
      case 'domains':
        return (
          <DomainsPage
            csrfToken={session.csrf_token}
            managedDomains={managedDomains}
            onManagedDomainsChange={setManagedDomains}
          />
        );
      case 'emails':
        return <EmailsPage csrfToken={session.csrf_token} managedDomains={managedDomains} />;
      case 'applications':
        return <ApplicationsPage csrfToken={session.csrf_token} />;
      case 'redeem':
        return <RedeemCodesPage csrfToken={session.csrf_token} />;
      case 'users':
      default:
        return <UsersPage csrfToken={session.csrf_token} managedDomains={managedDomains} />;
    }
  }

  const authorized = Boolean(session?.authenticated && session.authorized && session.user && session.csrf_token);

  if (!authorized) {
    return (
      <div className="relative min-h-screen overflow-x-hidden font-sans text-slate-900 transition-colors duration-500 dark:text-white">
        <div
          className="fixed inset-0 z-[-2] bg-cover bg-center bg-no-repeat transition-all duration-1000 dark:brightness-[0.28]"
          style={{ backgroundImage: 'url(https://www.loliapi.com/acg/)' }}
        />
        <div className="fixed inset-0 z-[-1] bg-white/45 backdrop-blur-[2px] dark:bg-black/45" />
        <AdminLogin
          error={sessionError}
          isDark={isDark}
          isLoading={sessionLoading}
          loginURL={loginURL}
          onLogout={session?.authenticated ? handleLogout : undefined}
          onToggleTheme={() => setIsDark((value) => !value)}
          currentUser={session?.user}
        />
      </div>
    );
  }

  return (
    <div className="relative min-h-screen overflow-x-hidden font-sans text-slate-900 transition-colors duration-500 dark:text-white">
      <div
        className="fixed inset-0 z-[-2] bg-cover bg-center bg-no-repeat transition-all duration-1000 dark:brightness-[0.28]"
        style={{ backgroundImage: 'url(https://www.loliapi.com/acg/)' }}
      />
      <div className="fixed inset-0 z-[-1] bg-white/45 backdrop-blur-[2px] dark:bg-black/45" />

      <AdminNavbar
        activeTab={activeTab}
        onTabChange={setActiveTab}
        isDark={isDark}
        onToggleTheme={() => setIsDark((value) => !value)}
        onLogout={handleLogout}
      />

      <div className="relative z-10 px-4 pb-24 pt-24 sm:px-6">
        <div className="mx-auto mb-6 flex max-w-7xl items-start gap-3 rounded-[28px] border border-amber-300/35 bg-amber-50/75 px-5 py-4 text-sm text-amber-950 shadow-lg backdrop-blur-xl dark:border-amber-500/20 dark:bg-amber-950/35 dark:text-amber-100">
          <ShieldCheck size={18} className="mt-0.5 shrink-0" />
          <p>
            当前管理员控制台已接入真实后端权限模型。所有写操作都会经过服务端会话、管理员检查、CSRF 校验与审计日志记录。
          </p>
        </div>
        {renderContent()}
      </div>
    </div>
  );
}
