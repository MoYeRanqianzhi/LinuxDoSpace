import { startTransition, useEffect, useMemo, useState } from 'react';
import { ShieldCheck } from 'lucide-react';
import { AdminNavbar } from './components/AdminNavbar';
import { AdminLogin } from './pages/AdminLogin';
import { ApplicationsPage } from './pages/ApplicationsPage';
import { DomainsPage } from './pages/DomainsPage';
import { EmailsPage } from './pages/EmailsPage';
import { RedeemCodesPage } from './pages/RedeemCodesPage';
import { UsersPage } from './pages/UsersPage';
import type { AdminTabKey } from './types/admin';

// 本地存储键集中管理，避免散落在多个组件里。
const STORAGE_KEYS = {
  theme: 'linuxdospace-admin-theme',
  demoAuth: 'linuxdospace-admin-demo-auth',
} as const;

// tabFromHash 根据 location.hash 解析出当前页面标签。
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

// App 负责管理员端的主题、伪登录态和标签页切换。
export default function App() {
  const [isDark, setIsDark] = useState<boolean>(() => window.localStorage.getItem(STORAGE_KEYS.theme) === 'dark');
  const [isAuthenticated, setIsAuthenticated] = useState<boolean>(
    () => window.localStorage.getItem(STORAGE_KEYS.demoAuth) === '1',
  );
  const [activeTab, setActiveTab] = useState<AdminTabKey>(() => tabFromHash(window.location.hash));
  const [loginError, setLoginError] = useState('');

  // demoPassword 明确标注它只是前端演示口令，不代表真正的管理员鉴权。
  const demoPassword = useMemo(
    () => (import.meta.env.VITE_ADMIN_DEMO_PASSWORD?.trim() || 'linuxdospace-admin-demo'),
    [],
  );

  // 同步深色模式类名，继续沿用现有 UI 的 dark 机制。
  useEffect(() => {
    document.documentElement.classList.toggle('dark', isDark);
    window.localStorage.setItem(STORAGE_KEYS.theme, isDark ? 'dark' : 'light');
  }, [isDark]);

  // 当前标签页变化时写回 hash，便于单页静态部署后刷新保持当前位置。
  useEffect(() => {
    const nextHash = `#${activeTab}`;
    if (window.location.hash !== nextHash) {
      window.history.replaceState(null, '', nextHash);
    }
  }, [activeTab]);

  // 处理浏览器前进后退，让 hash 与页面状态保持同步。
  useEffect(() => {
    const handleHashChange = () => {
      startTransition(() => {
        setActiveTab(tabFromHash(window.location.hash));
      });
    };

    window.addEventListener('hashchange', handleHashChange);
    return () => window.removeEventListener('hashchange', handleHashChange);
  }, []);

  // handleLogin 仅做演示密码校验，并把结果写入本地存储。
  function handleLogin(password: string) {
    if (password !== demoPassword) {
      setLoginError('演示口令不正确，请检查 Cloudflare Pages 环境变量或使用默认口令。');
      return;
    }

    setLoginError('');
    setIsAuthenticated(true);
    window.localStorage.setItem(STORAGE_KEYS.demoAuth, '1');
  }

  // handleLogout 清理演示登录态，避免误导成真实后台权限。
  function handleLogout() {
    setIsAuthenticated(false);
    window.localStorage.removeItem(STORAGE_KEYS.demoAuth);
  }

  // renderContent 根据标签页渲染不同的管理页面。
  function renderContent() {
    switch (activeTab) {
      case 'domains':
        return <DomainsPage />;
      case 'emails':
        return <EmailsPage />;
      case 'applications':
        return <ApplicationsPage />;
      case 'redeem':
        return <RedeemCodesPage />;
      case 'users':
      default:
        return <UsersPage />;
    }
  }

  if (!isAuthenticated) {
    return (
      <div className="relative min-h-screen overflow-x-hidden font-sans text-slate-900 transition-colors duration-500 dark:text-white">
        <div
          className="fixed inset-0 z-[-2] bg-cover bg-center bg-no-repeat transition-all duration-1000 dark:brightness-[0.28]"
          style={{ backgroundImage: 'url(https://www.loliapi.com/acg/)' }}
        />
        <div className="fixed inset-0 z-[-1] bg-white/45 backdrop-blur-[2px] dark:bg-black/45" />
        <AdminLogin
          error={loginError}
          isDark={isDark}
          onLogin={handleLogin}
          onToggleTheme={() => setIsDark((value) => !value)}
          passwordHint={demoPassword}
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
            当前管理员站点仍是独立 UI 原型，已适配为可单独部署的 React 项目，但暂未接入真实后台鉴权与管理 API。
          </p>
        </div>
        {renderContent()}
      </div>
    </div>
  );
}
