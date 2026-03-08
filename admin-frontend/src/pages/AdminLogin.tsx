import { ArrowRight, LogOut, Moon, Shield, Sun } from 'lucide-react';
import { motion } from 'motion/react';
import { GlassCard } from '../components/GlassCard';
import type { AdminUser } from '../types/admin';

// AdminLoginProps describes the data required by the standalone admin login page.
interface AdminLoginProps {
  error: string;
  isDark: boolean;
  isLoading: boolean;
  loginURL: string;
  onLogout?: () => void;
  onToggleTheme: () => void;
  currentUser?: AdminUser;
}

// AdminLogin renders the administrator-only Linux Do login entry.
export function AdminLogin({ error, isDark, isLoading, loginURL, onLogout, onToggleTheme, currentUser }: AdminLoginProps) {
  return (
    <div className="relative flex min-h-screen items-center justify-center px-4 py-10">
      <button
        onClick={onToggleTheme}
        className="absolute right-6 top-6 rounded-full bg-white/40 p-3 text-slate-700 shadow-lg backdrop-blur-md transition hover:bg-white/65 dark:bg-white/10 dark:text-slate-200 dark:hover:bg-white/15"
        aria-label="切换主题"
      >
        {isDark ? <Sun size={22} /> : <Moon size={22} />}
      </button>

      <motion.div
        initial={{ opacity: 0, y: 24, scale: 0.98 }}
        animate={{ opacity: 1, y: 0, scale: 1 }}
        transition={{ type: 'spring', damping: 24, stiffness: 220 }}
        className="w-full max-w-lg"
      >
        <GlassCard className="p-8 sm:p-10">
          <div className="mb-8 flex flex-col items-center text-center">
            <div className="mb-4 flex h-18 w-18 items-center justify-center rounded-[24px] bg-gradient-to-br from-red-500 to-orange-500 text-white shadow-xl">
              <Shield size={34} />
            </div>
            <h1 className="text-3xl font-bold text-slate-900 dark:text-white">LinuxDoSpace Admin</h1>
            <p className="mt-3 max-w-md text-sm leading-6 text-slate-500 dark:text-slate-300">
              管理员控制台已接入真实后端会话与权限检查。仅被站点授予管理员权限的 Linux Do 账号可以进入。
            </p>
          </div>

          <div className="space-y-5">
            {currentUser ? (
              <div className="rounded-2xl border border-amber-300/50 bg-amber-50/80 px-4 py-3 text-sm text-amber-800 dark:border-amber-500/25 dark:bg-amber-950/35 dark:text-amber-100">
                当前已登录账号：<span className="font-semibold">{currentUser.username}</span>，但该账号没有管理员权限。
              </div>
            ) : null}

            {error ? (
              <div className="rounded-2xl border border-red-300/50 bg-red-50/80 px-4 py-3 text-sm text-red-700 dark:border-red-500/20 dark:bg-red-950/30 dark:text-red-200">
                {error}
              </div>
            ) : null}

            <a
              href={loginURL}
              className={`flex w-full items-center justify-center gap-2 rounded-2xl bg-gradient-to-r from-red-500 to-orange-500 px-5 py-3 font-medium text-white shadow-lg transition hover:from-red-600 hover:to-orange-600 ${
                isLoading ? 'pointer-events-none opacity-60' : ''
              }`}
            >
              <span>{isLoading ? '正在检查会话...' : '使用 Linux Do 管理员登录'}</span>
              <ArrowRight size={18} />
            </a>

            {currentUser && onLogout ? (
              <button
                onClick={onLogout}
                className="flex w-full items-center justify-center gap-2 rounded-2xl bg-slate-100 px-5 py-3 font-medium text-slate-700 transition hover:bg-slate-200 dark:bg-slate-800 dark:text-slate-100 dark:hover:bg-slate-700"
              >
                <LogOut size={18} />
                <span>退出当前账号</span>
              </button>
            ) : null}
          </div>
        </GlassCard>
      </motion.div>
    </div>
  );
}
