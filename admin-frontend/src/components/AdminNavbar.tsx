import { useEffect, useState } from 'react';
import {
  Cloud,
  FileText,
  LogOut,
  Mail,
  Menu,
  Moon,
  ShieldAlert,
  Sun,
  Ticket,
  Users,
  X,
} from 'lucide-react';
import { AnimatePresence, motion } from 'motion/react';
import type { AdminTabKey } from '../types/admin';

interface AdminNavbarProps {
  activeTab: AdminTabKey;
  onTabChange: (tab: AdminTabKey) => void;
  isDark: boolean;
  onToggleTheme: () => void;
  onLogout: () => void;
}

const navItems: Array<{ id: AdminTabKey; label: string; icon: typeof Users }> = [
  { id: 'users', label: '用户管理', icon: Users },
  { id: 'domains', label: '域名管理', icon: Cloud },
  { id: 'emails', label: '邮箱管理', icon: Mail },
  { id: 'applications', label: '权限申请', icon: FileText },
  { id: 'redeem', label: '兑换码', icon: Ticket },
];

export function AdminNavbar({ activeTab, onTabChange, isDark, onToggleTheme, onLogout }: AdminNavbarProps) {
  const [isMobileMenuOpen, setIsMobileMenuOpen] = useState(false);

  useEffect(() => {
    setIsMobileMenuOpen(false);
  }, [activeTab]);

  return (
    <>
      <motion.nav
        initial={{ y: -40, opacity: 0 }}
        animate={{ y: 0, opacity: 1 }}
        className="fixed inset-x-0 top-0 z-40 px-4 py-4 sm:px-6"
      >
        <div className="mx-auto flex max-w-7xl items-center justify-between rounded-full border border-red-500/15 bg-white/45 px-4 py-3 shadow-xl backdrop-blur-xl dark:border-red-500/10 dark:bg-red-950/15 sm:px-6">
          <button className="flex items-center gap-3" onClick={() => onTabChange('users')}>
            <span className="flex h-11 w-11 items-center justify-center rounded-full bg-gradient-to-br from-red-500 to-orange-500 text-white shadow-lg">
              <ShieldAlert size={20} />
            </span>
            <span className="hidden text-left sm:block">
              <span className="block text-sm font-semibold uppercase tracking-[0.28em] text-red-500/80">Admin</span>
              <span className="block text-lg font-bold text-slate-900 dark:text-white">LinuxDoSpace Console</span>
            </span>
          </button>

          <div className="hidden items-center gap-2 xl:flex">
            {navItems.map((item) => {
              const Icon = item.icon;
              const isActive = item.id === activeTab;
              return (
                <button
                  key={item.id}
                  onClick={() => onTabChange(item.id)}
                  className={`flex items-center gap-2 rounded-full px-4 py-2 text-sm font-medium transition-all ${
                    isActive
                      ? 'bg-red-500/15 text-red-600 shadow-sm dark:bg-red-500/25 dark:text-red-300'
                      : 'text-slate-600 hover:bg-white/40 dark:text-slate-300 dark:hover:bg-white/5'
                  }`}
                >
                  <Icon size={17} />
                  <span>{item.label}</span>
                </button>
              );
            })}
          </div>

          <div className="hidden items-center gap-3 xl:flex">
            <button
              onClick={onToggleTheme}
              className="rounded-full bg-white/45 p-2.5 text-slate-700 transition hover:bg-white/70 dark:bg-white/10 dark:text-slate-200 dark:hover:bg-white/15"
              aria-label="切换主题"
            >
              {isDark ? <Sun size={19} /> : <Moon size={19} />}
            </button>
            <button
              onClick={onLogout}
              className="flex items-center gap-2 rounded-full bg-slate-900 px-5 py-2.5 text-sm font-medium text-white transition hover:bg-black"
            >
              <LogOut size={17} />
              <span>退出登录</span>
            </button>
          </div>

          <div className="flex items-center gap-2 xl:hidden">
            <button
              onClick={onToggleTheme}
              className="rounded-full bg-white/45 p-2.5 text-slate-700 transition hover:bg-white/70 dark:bg-white/10 dark:text-slate-200 dark:hover:bg-white/15"
              aria-label="切换主题"
            >
              {isDark ? <Sun size={19} /> : <Moon size={19} />}
            </button>
            <button
              onClick={() => setIsMobileMenuOpen(true)}
              className="rounded-full bg-white/45 p-2.5 text-slate-700 transition hover:bg-white/70 dark:bg-white/10 dark:text-slate-200 dark:hover:bg-white/15"
              aria-label="打开导航菜单"
            >
              <Menu size={19} />
            </button>
          </div>
        </div>
      </motion.nav>

      <AnimatePresence>
        {isMobileMenuOpen ? (
          <>
            <motion.button
              type="button"
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              className="fixed inset-0 z-50 bg-slate-950/45 backdrop-blur-sm xl:hidden"
              onClick={() => setIsMobileMenuOpen(false)}
              aria-label="关闭菜单背景遮罩"
            />
            <motion.aside
              initial={{ x: '100%' }}
              animate={{ x: 0 }}
              exit={{ x: '100%' }}
              transition={{ type: 'spring', damping: 22, stiffness: 220 }}
              className="fixed right-0 top-0 z-[60] flex h-full w-72 flex-col border-l border-white/15 bg-white/85 p-5 backdrop-blur-2xl dark:bg-slate-950/90 xl:hidden"
            >
              <div className="mb-5 flex items-center justify-between">
                <div>
                  <div className="text-xs uppercase tracking-[0.28em] text-red-500">Console</div>
                  <div className="mt-1 text-lg font-bold text-slate-900 dark:text-white">管理员菜单</div>
                </div>
                <button
                  onClick={() => setIsMobileMenuOpen(false)}
                  className="rounded-full bg-slate-100 p-2 text-slate-500 dark:bg-slate-800 dark:text-slate-300"
                  aria-label="关闭菜单"
                >
                  <X size={18} />
                </button>
              </div>

              <div className="custom-scrollbar flex flex-1 flex-col gap-2 overflow-y-auto">
                {navItems.map((item) => {
                  const Icon = item.icon;
                  const isActive = item.id === activeTab;
                  return (
                    <button
                      key={item.id}
                      onClick={() => onTabChange(item.id)}
                      className={`flex items-center gap-3 rounded-2xl px-4 py-3 text-left text-sm transition ${
                        isActive
                          ? 'bg-red-500/12 font-medium text-red-600 dark:bg-red-500/20 dark:text-red-300'
                          : 'text-slate-700 hover:bg-black/5 dark:text-slate-300 dark:hover:bg-white/5'
                      }`}
                    >
                      <Icon size={18} />
                      <span>{item.label}</span>
                    </button>
                  );
                })}
              </div>

              <button
                onClick={onLogout}
                className="mt-5 flex items-center justify-center gap-2 rounded-2xl bg-slate-900 px-4 py-3 text-sm font-medium text-white"
              >
                <LogOut size={18} />
                <span>退出登录</span>
              </button>
            </motion.aside>
          </>
        ) : null}
      </AnimatePresence>
    </>
  );
}
