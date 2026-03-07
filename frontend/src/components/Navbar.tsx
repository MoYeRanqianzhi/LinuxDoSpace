import { useEffect, useState } from 'react';
import { motion, AnimatePresence } from 'motion/react';
import {
  Moon,
  Sun,
  Cloud,
  Settings,
  Home,
  LogIn,
  ShieldAlert,
  Key,
  Mail,
  Menu,
  X,
  Github,
} from 'lucide-react';

// NavbarTab 约束导航栏允许切换到的页面标签。
type NavbarTab = 'home' | 'domains' | 'emails' | 'settings' | 'permissions' | 'supervision' | 'login';

// NavbarProps 描述导航栏与外层应用状态之间的交互契约。
interface NavbarProps {
  activeTab: string;
  setActiveTab: (tab: NavbarTab) => void;
  isDark: boolean;
  toggleTheme: () => void;
  authenticated: boolean;
  displayName?: string;
  onAuthAction: () => void;
}

// Navbar 负责承接新的导航布局、移动端抽屉菜单和顶部操作区。
// 它保留现有登录逻辑与 GitHub 入口，同时接入新设计中的多页面结构。
export function Navbar({
  activeTab,
  setActiveTab,
  isDark,
  toggleTheme,
  authenticated,
  displayName,
  onAuthAction,
}: NavbarProps) {
  // isMobileMenuOpen 控制移动端侧滑抽屉的展开与关闭。
  const [isMobileMenuOpen, setIsMobileMenuOpen] = useState(false);

  // 当页面切换后自动收起移动端抽屉，避免遮挡内容。
  useEffect(() => {
    setIsMobileMenuOpen(false);
  }, [activeTab]);

  // navItems 定义当前新 UI 中需要展示的全部主导航入口。
  const navItems = [
    { id: 'home' as const, label: '首页', icon: Home },
    { id: 'domains' as const, label: '域名分发', icon: Cloud },
    { id: 'emails' as const, label: '邮箱分发', icon: Mail },
    { id: 'settings' as const, label: '配置中心', icon: Settings },
    { id: 'permissions' as const, label: '权限申请', icon: Key },
    { id: 'supervision' as const, label: '共同监督', icon: ShieldAlert },
  ];

  // handleBrandClick 统一处理点击站点 Logo 后返回首页的动作。
  function handleBrandClick(): void {
    setActiveTab('home');
  }

  // handleAuthClick 统一处理右侧登录 / 控制台按钮点击。
  function handleAuthClick(): void {
    setIsMobileMenuOpen(false);
    onAuthAction();
  }

  return (
    <>
      <motion.nav
        initial={{ y: -50, opacity: 0 }}
        animate={{ y: 0, opacity: 1 }}
        className="fixed top-0 left-0 right-0 z-40 px-4 sm:px-6 py-4"
      >
        <div className="max-w-[85rem] mx-auto backdrop-blur-xl bg-white/30 dark:bg-black/30 border border-white/20 dark:border-white/10 shadow-lg rounded-full px-4 sm:px-6 py-3 flex items-center justify-between gap-3">
          <div className="flex items-center gap-3 cursor-pointer min-w-0" onClick={handleBrandClick}>
            <div className="w-10 h-10 rounded-full overflow-hidden bg-white/80 dark:bg-white/10 shadow-lg shrink-0 ring-1 ring-white/40 dark:ring-white/10">
              <img
                src="/ICON.png"
                alt="LinuxDoSpace 图标"
                className="w-full h-full object-cover"
              />
            </div>
            <span className="text-xl font-bold bg-clip-text text-transparent bg-gradient-to-r from-teal-500 to-emerald-600 dark:from-teal-400 dark:to-emerald-400 truncate">
              LinuxDoSpace
            </span>
          </div>

          <div className="hidden xl:flex items-center gap-2">
            {navItems.map((item) => {
              const Icon = item.icon;
              const isActive = activeTab === item.id;
              return (
                <button
                  key={item.id}
                  onClick={() => setActiveTab(item.id)}
                  className={`flex items-center gap-2 px-4 py-2 rounded-full transition-all duration-300 whitespace-nowrap ${
                    isActive
                      ? 'bg-white/50 dark:bg-white/10 text-teal-600 dark:text-teal-300 shadow-sm'
                      : 'text-gray-700 dark:text-gray-300 hover:bg-white/30 dark:hover:bg-white/5'
                  }`}
                >
                  <Icon size={18} className="shrink-0" />
                  <span className="font-medium">{item.label}</span>
                </button>
              );
            })}
          </div>

          <div className="hidden xl:flex items-center gap-3 shrink-0">
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
              onClick={handleAuthClick}
              className="flex items-center gap-2 px-5 py-2 rounded-full bg-gradient-to-r from-teal-500 to-emerald-500 hover:from-teal-600 hover:to-emerald-600 text-white font-medium shadow-lg hover:shadow-xl transition-all transform hover:-translate-y-0.5 whitespace-nowrap max-w-[11rem]"
            >
              {authenticated ? <Settings size={18} className="shrink-0" /> : <LogIn size={18} className="shrink-0" />}
              <span className="truncate">{authenticated ? displayName ?? '控制台' : '登录'}</span>
            </button>
          </div>

          <div className="flex xl:hidden items-center gap-2 shrink-0">
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
              onClick={() => setIsMobileMenuOpen(true)}
              className="p-2 rounded-full bg-white/30 dark:bg-white/10 text-gray-700 dark:text-gray-300 hover:bg-white/50 dark:hover:bg-white/20 transition-all"
            >
              <Menu size={20} />
            </button>
          </div>
        </div>
      </motion.nav>

      <AnimatePresence>
        {isMobileMenuOpen && (
          <>
            <motion.div
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              className="fixed inset-0 z-40 bg-black/40 backdrop-blur-sm xl:hidden"
              onClick={() => setIsMobileMenuOpen(false)}
            />

            <motion.aside
              initial={{ x: '100%' }}
              animate={{ x: 0 }}
              exit={{ x: '100%' }}
              transition={{ type: 'spring', stiffness: 300, damping: 30 }}
              className="fixed top-0 right-0 bottom-0 z-50 w-[20rem] max-w-[86vw] xl:hidden bg-white/75 dark:bg-gray-950/75 backdrop-blur-2xl border-l border-white/20 dark:border-white/10 shadow-2xl p-5 flex flex-col"
            >
              <div className="flex items-center justify-between mb-6">
                <div>
                  <div className="text-lg font-bold text-gray-900 dark:text-white">页面导航</div>
                  <div className="text-sm text-gray-500 dark:text-gray-400">新 UI 结构与旧功能并行适配中</div>
                </div>
                <button
                  onClick={() => setIsMobileMenuOpen(false)}
                  className="p-2 rounded-full bg-white/30 dark:bg-white/10 text-gray-700 dark:text-gray-300 hover:bg-white/50 dark:hover:bg-white/20 transition-all"
                >
                  <X size={18} />
                </button>
              </div>

              <div className="space-y-2">
                {navItems.map((item) => {
                  const Icon = item.icon;
                  const isActive = activeTab === item.id;
                  return (
                    <button
                      key={item.id}
                      onClick={() => setActiveTab(item.id)}
                      className={`w-full flex items-center gap-3 px-4 py-3 rounded-2xl transition-all text-left ${
                        isActive
                          ? 'bg-teal-500 text-white shadow-lg'
                          : 'bg-white/35 dark:bg-white/5 text-gray-700 dark:text-gray-300 hover:bg-white/50 dark:hover:bg-white/10'
                      }`}
                    >
                      <Icon size={18} className="shrink-0" />
                      <span className="font-medium">{item.label}</span>
                    </button>
                  );
                })}
              </div>

              <div className="mt-6 rounded-2xl bg-white/30 dark:bg-white/5 border border-white/20 dark:border-white/10 p-4 text-sm text-gray-600 dark:text-gray-300">
                新 UI 已接入更多页面。当前仍以后端已上线功能为准，未接后端的页面先展示前端结构与交互预览。
              </div>

              <div className="mt-auto pt-6">
                <button
                  onClick={handleAuthClick}
                  className="w-full flex items-center justify-center gap-2 px-5 py-3 rounded-2xl bg-gradient-to-r from-teal-500 to-emerald-500 hover:from-teal-600 hover:to-emerald-600 text-white font-medium shadow-lg transition-all"
                >
                  {authenticated ? <Settings size={18} /> : <LogIn size={18} />}
                  <span>{authenticated ? displayName ?? '进入控制台' : '登录 Linux Do'}</span>
                </button>
              </div>
            </motion.aside>
          </>
        )}
      </AnimatePresence>
    </>
  );
}
