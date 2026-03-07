import { useState, type FormEvent } from 'react';
import { ArrowRight, KeyRound, Moon, Shield, Sun } from 'lucide-react';
import { motion } from 'motion/react';
import { GlassCard } from '../components/GlassCard';

// AdminLoginProps 描述管理员登录页需要的交互参数。
interface AdminLoginProps {
  error: string;
  isDark: boolean;
  onLogin: (password: string) => void;
  onToggleTheme: () => void;
  passwordHint: string;
}

// AdminLogin 展示独立管理员端的演示登录入口。
export function AdminLogin({ error, isDark, onLogin, onToggleTheme, passwordHint }: AdminLoginProps) {
  const [password, setPassword] = useState('');

  // handleSubmit 把输入口令交给上层统一校验。
  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    onLogin(password.trim());
  }

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
              这是从 `new-ui-design` 中拆出的独立管理员前端。当前登录仅用于 UI 演示，不代表真实管理权限。
            </p>
          </div>

          <form onSubmit={handleSubmit} className="space-y-5">
            <div>
              <label className="mb-2 block text-sm font-medium text-slate-700 dark:text-slate-200">演示口令</label>
              <div className="relative">
                <span className="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-4 text-slate-400">
                  <KeyRound size={18} />
                </span>
                <input
                  type="password"
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  placeholder="输入演示口令"
                  className="w-full rounded-2xl border border-slate-200 bg-white/60 py-3 pl-11 pr-4 text-slate-900 outline-none transition focus:border-red-400 focus:ring-2 focus:ring-red-400/25 dark:border-slate-700 dark:bg-black/35 dark:text-white"
                />
              </div>
            </div>

            <div className="rounded-2xl border border-slate-200/70 bg-white/35 px-4 py-3 text-sm text-slate-600 dark:border-white/10 dark:bg-white/5 dark:text-slate-300">
              默认演示口令: <span className="font-mono font-semibold text-red-500">{passwordHint}</span>
            </div>

            {error ? (
              <div className="rounded-2xl border border-red-300/50 bg-red-50/80 px-4 py-3 text-sm text-red-700 dark:border-red-500/20 dark:bg-red-950/30 dark:text-red-200">
                {error}
              </div>
            ) : null}

            <button
              type="submit"
              className="flex w-full items-center justify-center gap-2 rounded-2xl bg-gradient-to-r from-red-500 to-orange-500 px-5 py-3 font-medium text-white shadow-lg transition hover:from-red-600 hover:to-orange-600 disabled:cursor-not-allowed disabled:opacity-60"
              disabled={!password.trim()}
            >
              <span>进入控制台</span>
              <ArrowRight size={18} />
            </button>
          </form>
        </GlassCard>
      </motion.div>
    </div>
  );
}
