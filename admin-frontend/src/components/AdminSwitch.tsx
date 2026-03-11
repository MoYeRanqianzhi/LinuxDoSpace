// AdminSwitchAccent 用来约束管理端开关允许使用的强调色。
// 不同页面可以复用同一组件，同时保留各自页面的主色调。
type AdminSwitchAccent = 'amber' | 'blue' | 'cyan' | 'fuchsia' | 'indigo' | 'red';

interface AdminSwitchProps {
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  label: string;
  description?: string;
  disabled?: boolean;
  accent?: AdminSwitchAccent;
  className?: string;
}

// checkedTrackClassByAccent 负责把业务色名映射为轨道颜色。
// 集中放在这里可以避免每个页面都手写一遍颜色判断。
const checkedTrackClassByAccent: Record<AdminSwitchAccent, string> = {
  amber: 'border-amber-300/70 bg-amber-500 focus:ring-amber-400/40 shadow-[0_8px_20px_rgba(245,158,11,0.28)]',
  blue: 'border-blue-300/70 bg-blue-500 focus:ring-blue-400/40 shadow-[0_8px_20px_rgba(59,130,246,0.28)]',
  cyan: 'border-cyan-300/70 bg-cyan-500 focus:ring-cyan-400/40 shadow-[0_8px_20px_rgba(6,182,212,0.28)]',
  fuchsia: 'border-fuchsia-300/70 bg-fuchsia-500 focus:ring-fuchsia-400/40 shadow-[0_8px_20px_rgba(217,70,239,0.28)]',
  indigo: 'border-indigo-300/70 bg-indigo-500 focus:ring-indigo-400/40 shadow-[0_8px_20px_rgba(99,102,241,0.28)]',
  red: 'border-red-300/70 bg-red-500 focus:ring-red-400/40 shadow-[0_8px_20px_rgba(239,68,68,0.28)]',
};

// AdminSwitch 统一替换管理端各处的原生 checkbox。
// 这样可以消除浏览器默认控件样式导致的渲染割裂问题，并保持和现有管理台玻璃态风格一致。
export function AdminSwitch({
  checked,
  onCheckedChange,
  label,
  description = '',
  disabled = false,
  accent = 'blue',
  className = '',
}: AdminSwitchProps) {
  return (
    <div
      className={[
        'flex items-center justify-between gap-4 rounded-2xl border border-slate-200 bg-white/70 px-4 py-3 text-sm text-slate-700 dark:border-slate-700 dark:bg-black/35 dark:text-slate-200',
        disabled ? 'opacity-70' : '',
        className,
      ].join(' ').trim()}
    >
      <div className="min-w-0 flex-1">
        <div className="font-medium text-slate-900 dark:text-white">{label}</div>
        {description ? <div className="mt-1 text-xs leading-6 text-slate-500 dark:text-slate-400">{description}</div> : null}
      </div>

      <button
        type="button"
        role="switch"
        aria-checked={checked}
        aria-label={label}
        disabled={disabled}
        onClick={() => onCheckedChange(!checked)}
        className={[
          'relative inline-flex h-8 w-14 shrink-0 items-center rounded-full border p-0.5 transition-all duration-200 focus:outline-none focus:ring-2',
          checked
            ? checkedTrackClassByAccent[accent]
            : 'border-white/35 bg-slate-300/90 shadow-inner focus:ring-slate-300/35 dark:border-white/15 dark:bg-slate-700/90 dark:focus:ring-slate-600/35',
          disabled ? 'cursor-not-allowed opacity-70' : 'hover:scale-[1.02]',
        ].join(' ').trim()}
      >
        <span
          className={[
            'absolute left-0.5 top-0.5 h-6 w-6 rounded-full bg-white shadow-[0_4px_12px_rgba(15,23,42,0.18)] transition-transform duration-200 will-change-transform',
            checked ? 'translate-x-7' : 'translate-x-0',
          ].join(' ').trim()}
        />
        <span className="sr-only">{checked ? '已开启' : '已关闭'}</span>
      </button>
    </div>
  );
}
