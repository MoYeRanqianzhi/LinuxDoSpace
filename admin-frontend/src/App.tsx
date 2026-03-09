import { startTransition, useEffect, useMemo, useState } from 'react';
import { ShieldCheck } from 'lucide-react';
import { AdminNavbar } from './components/AdminNavbar';
import { APIError, adminAuthInvalidatedEvent, getAdminLoginURL, getAdminSession, logout, verifyAdminPassword } from './lib/api';
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

const text = {
  notAdmin:
    '\u5f53\u524d Linux Do \u8d26\u53f7\u6ca1\u6709\u88ab\u6388\u4e88\u7ba1\u7406\u5458\u6743\u9650\uff0c\u8bf7\u5207\u6362\u8d26\u53f7\u540e\u91cd\u8bd5\u3002',
  forbidden: '\u5f53\u524d\u8d26\u53f7\u5df2\u88ab\u62d2\u7edd\u8bbf\u95ee\u7ba1\u7406\u5458\u63a7\u5236\u53f0\u3002',
  sessionExpired: '\u7ba1\u7406\u5458\u4f1a\u8bdd\u5df2\u5931\u6548\uff0c\u8bf7\u91cd\u65b0\u767b\u5f55\u3002',
  oauthUnavailable:
    '\u540e\u7aef\u5f53\u524d\u65e0\u6cd5\u5b8c\u6210 Linux Do \u767b\u5f55\uff0c\u8bf7\u7a0d\u540e\u91cd\u8bd5\u3002',
  loggedInButNotAdmin: '\u5f53\u524d\u8d26\u53f7\u5df2\u767b\u5f55\uff0c\u4f46\u6ca1\u6709\u7ba1\u7406\u5458\u6743\u9650\u3002',
  backendUnavailable: '\u65e0\u6cd5\u8fde\u63a5\u7ba1\u7406\u5458\u540e\u7aef\u3002',
  passwordVerifyFailed: '\u7ba1\u7406\u5458\u5bc6\u7801\u9a8c\u8bc1\u5931\u8d25\uff0c\u8bf7\u7a0d\u540e\u91cd\u8bd5\u3002',
  banner:
    '\u5f53\u524d\u7ba1\u7406\u5458\u63a7\u5236\u53f0\u5df2\u63a5\u5165\u771f\u5b9e\u540e\u7aef\u6743\u9650\u6a21\u578b\u3002\u6240\u6709\u5199\u64cd\u4f5c\u90fd\u4f1a\u7ecf\u8fc7\u670d\u52a1\u7aef\u4f1a\u8bdd\u3001\u7ba1\u7406\u5458\u68c0\u67e5\u3001\u4e8c\u6b21\u5bc6\u7801\u9a8c\u8bc1\u3001CSRF \u6821\u9a8c\u4e0e\u5ba1\u8ba1\u65e5\u5fd7\u8bb0\u5f55\u3002',
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
      return text.notAdmin;
    case 'forbidden':
      return text.forbidden;
    case 'unauthorized':
      return text.sessionExpired;
    case 'service_unavailable':
      return text.oauthUnavailable;
    default:
      return raw ? `\u7ba1\u7406\u5458\u767b\u5f55\u5931\u8d25\uff1a${raw}` : '';
  }
}

function AdminBackground() {
  return (
    <>
      <div className="fixed inset-0 z-[-3] bg-[radial-gradient(circle_at_top_left,_rgba(252,211,77,0.3),_transparent_40%),radial-gradient(circle_at_top_right,_rgba(56,189,248,0.22),_transparent_38%),linear-gradient(160deg,_#f8fafc_0%,_#e2e8f0_48%,_#cbd5e1_100%)] dark:bg-[radial-gradient(circle_at_top_left,_rgba(245,158,11,0.18),_transparent_40%),radial-gradient(circle_at_top_right,_rgba(34,211,238,0.14),_transparent_38%),linear-gradient(160deg,_#020617_0%,_#0f172a_52%,_#111827_100%)]" />
      <div className="fixed inset-0 z-[-2] overflow-hidden">
        <div className="absolute -left-16 top-12 h-64 w-64 rounded-full bg-amber-300/40 blur-3xl dark:bg-amber-500/20" />
        <div className="absolute right-[-5rem] top-1/4 h-72 w-72 rounded-full bg-sky-300/35 blur-3xl dark:bg-cyan-400/20" />
        <div className="absolute bottom-[-6rem] left-1/3 h-80 w-80 rounded-full bg-emerald-300/20 blur-3xl dark:bg-emerald-400/15" />
      </div>
      <div className="fixed inset-0 z-[-1] bg-white/45 backdrop-blur-[2px] dark:bg-slate-950/50" />
    </>
  );
}

export default function App() {
  const [isDark, setIsDark] = useState<boolean>(() => window.localStorage.getItem(STORAGE_KEYS.theme) === 'dark');
  const [activeTab, setActiveTab] = useState<AdminTabKey>(() => tabFromHash(window.location.hash));
  const [session, setSession] = useState<AdminSessionResponse | null>(null);
  const [managedDomains, setManagedDomains] = useState<ManagedDomain[]>([]);
  const [sessionLoading, setSessionLoading] = useState(true);
  const [passwordLoading, setPasswordLoading] = useState(false);
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
        setSessionError(text.loggedInButNotAdmin);
      } else if (data.authenticated) {
        setSessionError('');
      }
    } catch (error) {
      if (error instanceof APIError) {
        setSessionError(error.message);
      } else {
        setSessionError(text.backendUnavailable);
      }
    } finally {
      setSessionLoading(false);
    }
  }

  useEffect(() => {
    void loadSession();
  }, []);

  useEffect(() => {
    const handleAuthInvalidated = () => {
      void loadSession();
    };

    window.addEventListener(adminAuthInvalidatedEvent, handleAuthInvalidated);
    return () => window.removeEventListener(adminAuthInvalidatedEvent, handleAuthInvalidated);
  }, []);

  async function handleLogout() {
    if (!session?.csrf_token) {
      setSession(null);
      setManagedDomains([]);
      return;
    }
    try {
      await logout(session.csrf_token);
    } catch (error) {
      if (error instanceof APIError && (error.code === 'unauthorized' || error.code === 'forbidden')) {
        setSession(null);
        setManagedDomains([]);
        setSessionError('');
        return;
      }
      setSessionError(error instanceof APIError ? error.message : text.backendUnavailable);
      return;
    }
    setSession(null);
    setManagedDomains([]);
    setSessionError('');
  }

  async function handleVerifyPassword(password: string) {
    if (!session?.csrf_token) {
      setSessionError(text.sessionExpired);
      return;
    }

    try {
      setPasswordLoading(true);
      const data = await verifyAdminPassword(password, session.csrf_token);
      setSession(data);
      setManagedDomains(data.managed_domains ?? []);
      setSessionError('');
    } catch (error) {
      if (error instanceof APIError) {
        setSessionError(error.message);
      } else {
        setSessionError(text.passwordVerifyFailed);
      }
    } finally {
      setPasswordLoading(false);
    }
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

  const requiresPasswordVerification = Boolean(
    session?.authenticated && session.authorized && !session.password_verified && session.user && session.csrf_token,
  );
  const authorized = Boolean(
    session?.authenticated && session.authorized && session.password_verified && session.user && session.csrf_token,
  );

  if (!authorized) {
    return (
      <div className="relative min-h-screen overflow-x-hidden font-sans text-slate-900 transition-colors duration-500 dark:text-white">
        <AdminBackground />
        <AdminLogin
          error={sessionError}
          isDark={isDark}
          isLoading={sessionLoading}
          isVerifyingPassword={passwordLoading}
          loginURL={loginURL}
          onLogout={session?.authenticated ? handleLogout : undefined}
          onToggleTheme={() => setIsDark((value) => !value)}
          onVerifyPassword={handleVerifyPassword}
          currentUser={session?.user}
          requiresPasswordVerification={requiresPasswordVerification}
        />
      </div>
    );
  }

  return (
    <div className="relative min-h-screen overflow-x-hidden font-sans text-slate-900 transition-colors duration-500 dark:text-white">
      <AdminBackground />

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
          <p>{text.banner}</p>
        </div>
        {renderContent()}
      </div>
    </div>
  );
}
