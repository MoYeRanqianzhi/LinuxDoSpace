import { useEffect, useState, type FormEvent } from 'react';
import { ArrowRight, KeyRound, LogOut, Moon, Shield, Sun } from 'lucide-react';
import { motion } from 'motion/react';
import { GlassCard } from '../components/GlassCard';
import type { AdminUser } from '../types/admin';

const text = {
  toggleTheme: '\u5207\u6362\u4e3b\u9898',
  heading: 'LinuxDoSpace Admin',
  intro:
    '\u7ba1\u7406\u5458\u63a7\u5236\u53f0\u5df2\u63a5\u5165\u771f\u5b9e\u540e\u7aef\u4f1a\u8bdd\u4e0e\u6743\u9650\u68c0\u67e5\u3002\u4ec5\u88ab\u7ad9\u70b9\u6388\u4e88\u7ba1\u7406\u5458\u6743\u9650\u7684 Linux Do \u8d26\u53f7\u53ef\u4ee5\u8fdb\u5165\u3002',
  passwordIntro:
    '\u5df2\u901a\u8fc7 Linux Do \u7ba1\u7406\u5458\u8eab\u4efd\u6821\u9a8c\u3002\u7ee7\u7eed\u8f93\u5165\u7ba1\u7406\u5458\u5bc6\u7801\u540e\uff0c\u624d\u4f1a\u653e\u884c\u771f\u6b63\u7684\u63a7\u5236\u53f0\u80fd\u529b\u3002',
  noAdminPrefix: '\u5f53\u524d\u5df2\u767b\u5f55\u8d26\u53f7\uff1a',
  noAdminSuffix: '\uff0c\u4f46\u8be5\u8d26\u53f7\u6ca1\u6709\u7ba1\u7406\u5458\u6743\u9650\u3002',
  verifyPrefix: '\u5f53\u524d\u7ba1\u7406\u5458\u8d26\u53f7\uff1a',
  verifySuffix:
    '\u3002\u4e3a\u907f\u514d\u4ec5\u9760 OAuth \u5373\u53ef\u8fdb\u5165\u540e\u53f0\uff0c\u8fd8\u9700\u518d\u5b8c\u6210\u4e00\u6b21\u5bc6\u7801\u9a8c\u8bc1\u3002',
  passwordLabel: '\u7ba1\u7406\u5458\u5bc6\u7801',
  passwordPlaceholder: '\u8f93\u5165\u989d\u5916\u7ba1\u7406\u5458\u5bc6\u7801',
  verifyingPassword: '\u6b63\u5728\u9a8c\u8bc1\u7ba1\u7406\u5458\u5bc6\u7801...',
  verifyPassword: '\u9a8c\u8bc1\u7ba1\u7406\u5458\u5bc6\u7801',
  checkingSession: '\u6b63\u5728\u68c0\u67e5\u4f1a\u8bdd...',
  login: '\u4f7f\u7528 Linux Do \u7ba1\u7406\u5458\u767b\u5f55',
  logout: '\u9000\u51fa\u5f53\u524d\u8d26\u53f7',
} as const;

// AdminLoginProps describes the data required by the standalone admin login page.
interface AdminLoginProps {
  error: string;
  isDark: boolean;
  isLoading: boolean;
  isVerifyingPassword: boolean;
  loginURL: string;
  onLogout?: () => void;
  onToggleTheme: () => void;
  onVerifyPassword?: (password: string) => Promise<void>;
  currentUser?: AdminUser;
  requiresPasswordVerification: boolean;
}

// AdminLogin renders the administrator-only Linux Do login entry.
export function AdminLogin({
  error,
  isDark,
  isLoading,
  isVerifyingPassword,
  loginURL,
  onLogout,
  onToggleTheme,
  onVerifyPassword,
  currentUser,
  requiresPasswordVerification,
}: AdminLoginProps) {
  const [password, setPassword] = useState('');

  useEffect(() => {
    setPassword('');
  }, [currentUser?.username, requiresPasswordVerification]);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!onVerifyPassword) {
      return;
    }
    await onVerifyPassword(password);
  }

  return (
    <div className="relative flex min-h-screen items-center justify-center px-4 py-10">
      <button
        onClick={onToggleTheme}
        className="absolute right-6 top-6 rounded-full bg-white/40 p-3 text-slate-700 shadow-lg backdrop-blur-md transition hover:bg-white/65 dark:bg-white/10 dark:text-slate-200 dark:hover:bg-white/15"
        aria-label={text.toggleTheme}
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
            <h1 className="text-3xl font-bold text-slate-900 dark:text-white">{text.heading}</h1>
            <p className="mt-3 max-w-md text-sm leading-6 text-slate-500 dark:text-slate-300">
              {requiresPasswordVerification ? text.passwordIntro : text.intro}
            </p>
          </div>

          <div className="space-y-5">
            {currentUser && !requiresPasswordVerification ? (
              <div className="rounded-2xl border border-amber-300/50 bg-amber-50/80 px-4 py-3 text-sm text-amber-800 dark:border-amber-500/25 dark:bg-amber-950/35 dark:text-amber-100">
                {text.noAdminPrefix}
                <span className="font-semibold">{currentUser.username}</span>
                {text.noAdminSuffix}
              </div>
            ) : null}

            {currentUser && requiresPasswordVerification ? (
              <div className="rounded-2xl border border-sky-300/50 bg-sky-50/80 px-4 py-3 text-sm text-sky-800 dark:border-sky-500/25 dark:bg-sky-950/35 dark:text-sky-100">
                {text.verifyPrefix}
                <span className="font-semibold">{currentUser.username}</span>
                {text.verifySuffix}
              </div>
            ) : null}

            {error ? (
              <div className="rounded-2xl border border-red-300/50 bg-red-50/80 px-4 py-3 text-sm text-red-700 dark:border-red-500/20 dark:bg-red-950/30 dark:text-red-200">
                {error}
              </div>
            ) : null}

            {requiresPasswordVerification ? (
              <form className="space-y-4" onSubmit={handleSubmit}>
                <label className="block space-y-2">
                  <span className="text-sm font-medium text-slate-700 dark:text-slate-200">{text.passwordLabel}</span>
                  <input
                    type="password"
                    value={password}
                    onChange={(event) => setPassword(event.target.value)}
                    autoComplete="current-password"
                    placeholder={text.passwordPlaceholder}
                    className="w-full rounded-2xl border border-white/60 bg-white/70 px-4 py-3 text-slate-900 shadow-inner outline-none transition placeholder:text-slate-400 focus:border-orange-400 focus:ring-2 focus:ring-orange-300/60 dark:border-white/10 dark:bg-slate-900/70 dark:text-white dark:placeholder:text-slate-500 dark:focus:border-orange-400 dark:focus:ring-orange-500/30"
                  />
                </label>

                <button
                  type="submit"
                  disabled={isVerifyingPassword || password.trim() === ''}
                  className="flex w-full items-center justify-center gap-2 rounded-2xl bg-gradient-to-r from-red-500 to-orange-500 px-5 py-3 font-medium text-white shadow-lg transition hover:from-red-600 hover:to-orange-600 disabled:cursor-not-allowed disabled:opacity-60"
                >
                  <KeyRound size={18} />
                  <span>{isVerifyingPassword ? text.verifyingPassword : text.verifyPassword}</span>
                </button>
              </form>
            ) : (
              <a
                href={loginURL}
                className={`flex w-full items-center justify-center gap-2 rounded-2xl bg-gradient-to-r from-red-500 to-orange-500 px-5 py-3 font-medium text-white shadow-lg transition hover:from-red-600 hover:to-orange-600 ${
                  isLoading ? 'pointer-events-none opacity-60' : ''
                }`}
              >
                <span>{isLoading ? text.checkingSession : text.login}</span>
                <ArrowRight size={18} />
              </a>
            )}

            {currentUser && onLogout ? (
              <button
                onClick={onLogout}
                className="flex w-full items-center justify-center gap-2 rounded-2xl bg-slate-100 px-5 py-3 font-medium text-slate-700 transition hover:bg-slate-200 dark:bg-slate-800 dark:text-slate-100 dark:hover:bg-slate-700"
              >
                <LogOut size={18} />
                <span>{text.logout}</span>
              </button>
            ) : null}
          </div>
        </GlassCard>
      </motion.div>
    </div>
  );
}
