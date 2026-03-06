import { Moon, Sun, Cloud, Settings, Home, LogIn, Github, Eye } from 'lucide-react';
import { motion } from 'motion/react';

// NavbarProps 描述导航栏和外层应用状态之间的交互契约。
interface NavbarProps {
  activeTab: string;
  setActiveTab: (tab: 'home' | 'domains' | 'supervision' | 'settings' | 'login') => void;
  isDark: boolean;
  toggleTheme: () => void;
  authenticated: boolean;
  displayName?: string;
  onAuthAction: () => void;
}

// Navbar 负责展示顶栏导航、主题切换和登录 / 控制台入口。
// 视觉风格沿用原有玻璃态设计，只把按钮行为切换到真实后端逻辑。
export function Navbar({
  activeTab,
  setActiveTab,
  isDark,
  toggleTheme,
  authenticated,
  displayName,
  onAuthAction,
}: NavbarProps) {
  // navItems 是当前前端已实现的四块主内容导航。
  const navItems = [
    { id: 'home' as const, label: '首页', icon: Home },
    { id: 'domains' as const, label: '域名分发', icon: Cloud },
    { id: 'supervision' as const, label: '共同监督', icon: Eye },
    { id: 'settings' as const, label: '配置中心', icon: Settings },
  ];

  return (
    <motion.nav
      initial={{ y: -50, opacity: 0 }}
      animate={{ y: 0, opacity: 1 }}
      className="fixed top-0 left-0 right-0 z-50 px-6 py-4"
    >
      <div className="max-w-6xl mx-auto backdrop-blur-xl bg-white/30 dark:bg-black/30 border border-white/20 dark:border-white/10 shadow-lg rounded-full px-6 py-3 flex items-center justify-between gap-4">
        <div className="flex items-center gap-3 cursor-pointer" onClick={() => setActiveTab('home')}>
          <div className="w-10 h-10 rounded-full bg-gradient-to-tr from-teal-400 to-emerald-500 flex items-center justify-center text-white font-bold text-xl shadow-lg">
            L
          </div>
          <span className="text-xl font-bold bg-clip-text text-transparent bg-gradient-to-r from-teal-500 to-emerald-600 dark:from-teal-400 dark:to-emerald-400 hidden sm:block">
            LinuxDoSpace
          </span>
        </div>

        <div className="flex items-center gap-2 sm:gap-6">
          {navItems.map((item) => {
            const Icon = item.icon;
            const isActive = activeTab === item.id;
            return (
              <button
                key={item.id}
                onClick={() => setActiveTab(item.id)}
                className={`flex items-center gap-2 px-4 py-2 rounded-full transition-all duration-300 ${
                  isActive
                    ? 'bg-white/50 dark:bg-white/10 text-teal-600 dark:text-teal-300 shadow-sm'
                    : 'text-gray-700 dark:text-gray-300 hover:bg-white/30 dark:hover:bg-white/5'
                }`}
              >
                <Icon size={18} />
                <span className="hidden md:block font-medium">{item.label}</span>
              </button>
            );
          })}
        </div>

        <div className="flex items-center gap-3">
          <button
            onClick={toggleTheme}
            className="p-2 rounded-full bg-white/30 dark:bg-white/10 text-gray-700 dark:text-gray-300 hover:bg-white/50 dark:hover:bg-white/20 transition-all"
          >
            {isDark ? <Sun size={20} /> : <Moon size={20} />}
          </button>
          <a
            href="https://github.com/MoYeRanqianzhi/LinuxDoSpace"
            target="_blank"
            rel="noreferrer"
            title="查看 GitHub 仓库"
            aria-label="查看 GitHub 仓库"
            className="p-2 rounded-full bg-white/30 dark:bg-white/10 text-gray-700 dark:text-gray-300 hover:bg-white/50 dark:hover:bg-white/20 transition-all"
          >
            <Github size={18} />
          </a>
          <button
            onClick={onAuthAction}
            className="flex items-center gap-2 px-5 py-2 rounded-full bg-gradient-to-r from-teal-500 to-emerald-500 hover:from-teal-600 hover:to-emerald-600 text-white font-medium shadow-lg hover:shadow-xl transition-all transform hover:-translate-y-0.5 max-w-[10rem]"
          >
            {authenticated ? <Settings size={18} /> : <LogIn size={18} />}
            <span className="hidden sm:block truncate">{authenticated ? displayName ?? '控制台' : '登录'}</span>
          </button>
        </div>
      </div>
    </motion.nav>
  );
}
