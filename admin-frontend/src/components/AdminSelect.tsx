import {
  Children,
  Fragment,
  isValidElement,
  useDeferredValue,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ChangeEvent,
  type PropsWithChildren,
  type ReactElement,
  type ReactNode,
  type SelectHTMLAttributes,
} from 'react';
import { ChevronDown, Search } from 'lucide-react';

// AdminSelectOption 描述一个已解析的 option 节点。
// 管理端现有调用点仍然直接传 `<option>`，这里统一转成更易处理的数据结构。
interface AdminSelectOption {
  value: string;
  label: string;
  disabled: boolean;
}

// OptionLikeProps 约束当前组件实际会读取的 option / fragment props 字段。
type OptionLikeProps = {
  value?: string | number;
  disabled?: boolean;
  children?: ReactNode;
};

// AdminSelectProps 保持与原生 select 的受控用法兼容，同时追加可选搜索能力。
interface AdminSelectProps extends PropsWithChildren<SelectHTMLAttributes<HTMLSelectElement>> {
  wrapperClassName?: string;
  searchable?: boolean;
  searchPlaceholder?: string;
  emptySearchLabel?: string;
}

// AdminSelect 使用自定义的玻璃态下拉层统一替换浏览器原生 select。
// 这样既能修复原生控件在不同平台上的渲染割裂，也能在长列表场景里直接加入搜索框。
export function AdminSelect({
  children,
  className = '',
  wrapperClassName = '',
  searchable = false,
  searchPlaceholder = '搜索选项',
  emptySearchLabel = '没有命中可选项',
  disabled = false,
  value,
  defaultValue,
  onChange,
  name,
  ...props
}: AdminSelectProps) {
  // isOpen 控制下拉面板开合。
  const [isOpen, setIsOpen] = useState(false);

  // searchKeyword 只在 searchable 场景下启用，用于过滤当前选项列表。
  const [searchKeyword, setSearchKeyword] = useState('');

  // containerRef 用于处理点击外部关闭。
  const containerRef = useRef<HTMLDivElement>(null);

  // searchInputRef 让搜索型下拉在展开后自动聚焦输入框。
  const searchInputRef = useRef<HTMLInputElement>(null);

  // currentValue 统一把 number/string value 转成字符串，以便与 option 做稳定比较。
  const currentValue = value != null ? String(value) : defaultValue != null ? String(defaultValue) : '';

  // options 负责把 JSX option 子节点解析成平铺结构，避免页面层重写全部调用方式。
  const options = useMemo(() => collectOptions(children), [children]);

  // selectedOption 表示当前受控值对应的选项，便于按钮上显示标签。
  const selectedOption = options.find((option) => option.value === currentValue);

  // deferredSearchKeyword 避免用户长列表搜索时每次敲击都同步阻塞整个页面渲染。
  const deferredSearchKeyword = useDeferredValue(searchKeyword);

  // filteredOptions 根据搜索词过滤面板中的候选项。
  const filteredOptions = useMemo(() => {
    const normalizedKeyword = deferredSearchKeyword.trim().toLowerCase();
    if (!normalizedKeyword) {
      return options;
    }

    return options.filter((option) => option.label.toLowerCase().includes(normalizedKeyword));
  }, [deferredSearchKeyword, options]);

  // 展开搜索型下拉时自动聚焦搜索框，关闭时清空上一次过滤词，避免误导下一次操作。
  useEffect(() => {
    if (isOpen && searchable) {
      window.setTimeout(() => searchInputRef.current?.focus(), 0);
      return;
    }
    setSearchKeyword('');
  }, [isOpen, searchable]);

  // 点击组件外部或按下 Escape 时关闭下拉层。
  useEffect(() => {
    function handlePointerDown(event: MouseEvent): void {
      if (!containerRef.current?.contains(event.target as Node)) {
        setIsOpen(false);
      }
    }

    function handleEscape(event: KeyboardEvent): void {
      if (event.key === 'Escape') {
        setIsOpen(false);
      }
    }

    document.addEventListener('mousedown', handlePointerDown);
    document.addEventListener('keydown', handleEscape);

    return () => {
      document.removeEventListener('mousedown', handlePointerDown);
      document.removeEventListener('keydown', handleEscape);
    };
  }, []);

  // handleSelectValue 把自定义选项点击转换成现有页面已经依赖的原生 onChange 形态。
  function handleSelectValue(nextValue: string): void {
    if (disabled) {
      return;
    }

    const syntheticEvent = {
      target: { value: nextValue, name },
      currentTarget: { value: nextValue, name },
    } as ChangeEvent<HTMLSelectElement>;

    onChange?.(syntheticEvent);
    setIsOpen(false);
  }

  return (
    <div ref={containerRef} className={`relative w-full ${wrapperClassName}`.trim()}>
      {/* 隐藏的原生 select 继续保留表单语义和受控值，但实际视觉交互完全交给自定义面板。 */}
      <select
        {...props}
        name={name}
        value={currentValue}
        disabled={disabled}
        onChange={onChange}
        className="sr-only"
        tabIndex={-1}
        aria-hidden="true"
      >
        {children}
      </select>

      <button
        type="button"
        aria-haspopup="listbox"
        aria-expanded={isOpen}
        disabled={disabled}
        onClick={() => setIsOpen((current) => !current)}
        className={[
          'flex w-full items-center justify-between gap-3 rounded-2xl border border-white/20 bg-white/70 px-4 py-3 text-left text-slate-900 shadow-sm backdrop-blur-md transition-all',
          'focus:outline-none focus:ring-2 focus:ring-cyan-400/25 dark:border-white/10 dark:bg-black/35 dark:text-white',
          'disabled:cursor-not-allowed disabled:opacity-60',
          isOpen ? 'border-cyan-400/50 shadow-lg' : 'hover:bg-white/85 dark:hover:bg-black/45',
          className,
        ].join(' ').trim()}
      >
        <span className={`min-w-0 flex-1 truncate ${selectedOption ? '' : 'text-slate-400 dark:text-slate-500'}`}>
          {selectedOption?.label ?? '请选择'}
        </span>
        <ChevronDown
          size={18}
          className={`shrink-0 text-slate-400 transition-transform duration-200 dark:text-slate-500 ${isOpen ? 'rotate-180' : ''}`}
        />
      </button>

      {isOpen ? (
        <div className="absolute z-50 mt-2 w-full overflow-hidden rounded-3xl border border-white/30 bg-white/90 shadow-2xl backdrop-blur-2xl dark:border-white/10 dark:bg-slate-950/88">
          {searchable ? (
            <div className="border-b border-slate-200/80 px-3 py-3 dark:border-white/10">
              <label className="relative block">
                <Search size={16} className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-slate-400 dark:text-slate-500" />
                <input
                  ref={searchInputRef}
                  value={searchKeyword}
                  onChange={(event) => setSearchKeyword(event.target.value)}
                  onKeyDown={(event) => event.stopPropagation()}
                  placeholder={searchPlaceholder}
                  className="w-full rounded-2xl border border-slate-200 bg-white/80 py-2.5 pl-10 pr-4 text-sm text-slate-900 outline-none transition focus:border-cyan-400 focus:ring-2 focus:ring-cyan-400/20 dark:border-slate-700 dark:bg-black/35 dark:text-white"
                />
              </label>
            </div>
          ) : null}

          <div className="max-h-72 overflow-y-auto py-2" role="listbox">
            {filteredOptions.length > 0 ? (
              filteredOptions.map((option) => {
                const isSelected = option.value === currentValue;

                return (
                  <button
                    key={`${option.value}:${option.label}`}
                    type="button"
                    role="option"
                    aria-selected={isSelected}
                    disabled={option.disabled}
                    onClick={() => handleSelectValue(option.value)}
                    className={[
                      'flex w-full items-center justify-between gap-3 px-4 py-3 text-left text-sm transition-colors',
                      option.disabled ? 'cursor-not-allowed opacity-50' : '',
                      isSelected
                        ? 'bg-cyan-500/10 font-semibold text-cyan-700 dark:bg-cyan-500/15 dark:text-cyan-300'
                        : 'text-slate-700 hover:bg-slate-100/80 dark:text-slate-200 dark:hover:bg-white/6',
                    ].join(' ').trim()}
                  >
                    <span className="min-w-0 flex-1 truncate">{option.label}</span>
                    {isSelected ? <span className="text-[11px] font-semibold uppercase tracking-[0.2em]">当前</span> : null}
                  </button>
                );
              })
            ) : (
              <div className="px-4 py-6 text-center text-sm text-slate-500 dark:text-slate-400">{emptySearchLabel}</div>
            )}
          </div>
        </div>
      ) : null}
    </div>
  );
}

// collectOptions 递归解析 JSX children，只提取当前管理端实际使用的 option 节点。
function collectOptions(children: ReactNode): AdminSelectOption[] {
  const result: AdminSelectOption[] = [];

  Children.forEach(children, (child) => {
    if (!isValidElement<OptionLikeProps>(child)) {
      return;
    }

    const element = child as ReactElement<OptionLikeProps>;

    if (element.type === Fragment) {
      result.push(...collectOptions(element.props.children));
      return;
    }

    if (element.type !== 'option') {
      return;
    }

    const optionValue = String(element.props.value ?? extractNodeText(element.props.children));
    const optionLabel = extractNodeText(element.props.children).trim() || optionValue;

    result.push({
      value: optionValue,
      label: optionLabel,
      disabled: Boolean(element.props.disabled),
    });
  });

  return result;
}

// extractNodeText 把 option 内部的 ReactNode 展平成纯文本标签。
function extractNodeText(node: ReactNode): string {
  if (typeof node === 'string' || typeof node === 'number') {
    return String(node);
  }

  if (Array.isArray(node)) {
    return node.map((item) => extractNodeText(item)).join('');
  }

  if (isValidElement<OptionLikeProps>(node)) {
    return extractNodeText((node as ReactElement<OptionLikeProps>).props.children);
  }

  return '';
}
