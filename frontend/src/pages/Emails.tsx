import { useEffect, useMemo, useRef, useState, type FormEvent } from 'react';
import { AnimatePresence, motion } from 'motion/react';
import {
  AlertCircle,
  ArrowRight,
  CheckCircle2,
  LoaderCircle,
  Mail,
  Plus,
  RefreshCw,
  Search,
  ShieldCheck,
  Sparkles,
  X,
} from 'lucide-react';
import { GlassCard } from '../components/GlassCard';
import { GlassSelect, type GlassSelectOption } from '../components/GlassSelect';
import { ToggleSwitch } from '../components/ToggleSwitch';
import {
  APIError,
  checkPublicEmailRouteAvailability,
  createMyEmailTarget,
  listMyEmailRoutes,
  listMyEmailTargets,
  listMyPermissions,
  submitPermissionApplication,
  upsertCatchAllEmailRoute,
  upsertDefaultEmailRoute,
} from '../lib/api';
import type {
  EmailRouteAvailabilityResult,
  ManagedDomain,
  PermissionStatus,
  User,
  UserEmailRoute,
  UserEmailTarget,
  UserPermission,
} from '../types/api';

interface EmailsProps {
  authenticated: boolean;
  sessionLoading: boolean;
  user?: User;
  publicDomains: ManagedDomain[];
  csrfToken?: string;
  onLogin: () => void;
}

type SearchStatus = 'idle' | 'checking' | 'available' | 'taken' | 'error';
type NoticeTone = 'error' | 'success' | 'info';

interface SectionNotice {
  tone: NoticeTone;
  message: string;
}

interface ChipDescriptor {
  label: string;
  className: string;
}

const catchAllPermissionKey = 'email_catch_all';
const fallbackRootDomain = 'linuxdo.space';

// Emails keeps mailbox search public while forcing authenticated users to bind
// forwarding targets first and only then select verified targets for routing.
export function Emails({ authenticated, sessionLoading, user, publicDomains, csrfToken, onLogin }: EmailsProps) {
  const [permission, setPermission] = useState<UserPermission | null>(null);
  const [routes, setRoutes] = useState<UserEmailRoute[]>([]);
  const [emailTargets, setEmailTargets] = useState<UserEmailTarget[]>([]);
  const [loading, setLoading] = useState(false);
  const [permissionError, setPermissionError] = useState('');
  const [routeError, setRouteError] = useState('');
  const [targetError, setTargetError] = useState('');

  const [searchPrefix, setSearchPrefix] = useState('');
  const [searchStatus, setSearchStatus] = useState<SearchStatus>('idle');
  const [searchResult, setSearchResult] = useState<EmailRouteAvailabilityResult | null>(null);
  const [searchMessage, setSearchMessage] = useState('');

  const [defaultTarget, setDefaultTarget] = useState('');
  const [defaultEnabled, setDefaultEnabled] = useState(false);
  const [savingDefault, setSavingDefault] = useState(false);
  const [defaultNotice, setDefaultNotice] = useState<SectionNotice | null>(null);

  const [catchAllTarget, setCatchAllTarget] = useState('');
  const [catchAllEnabled, setCatchAllEnabled] = useState(false);
  const [savingCatchAll, setSavingCatchAll] = useState(false);
  const [catchAllNotice, setCatchAllNotice] = useState<SectionNotice | null>(null);

  const [newTargetEmail, setNewTargetEmail] = useState('');
  const [creatingTarget, setCreatingTarget] = useState(false);
  const [targetNotice, setTargetNotice] = useState<SectionNotice | null>(null);

  const [applyingPermission, setApplyingPermission] = useState(false);
  const [pledgeModalOpen, setPledgeModalOpen] = useState(false);
  const loadRequestTokenRef = useRef(0);
  const searchRequestTokenRef = useRef(0);

  const normalizedUsername = useMemo(() => normalizePrefix(user?.username ?? ''), [user?.username]);
  const configuredRootDomain = useMemo(() => {
    const defaultManagedDomain = publicDomains.find((item) => item.is_default) ?? publicDomains[0];
    return defaultManagedDomain?.root_domain?.trim() || fallbackRootDomain;
  }, [publicDomains]);
  const defaultRoute = useMemo(() => {
    const existingRoute = routes.find((item) => item.kind === 'default');
    return existingRoute ?? (user ? buildImplicitDefaultRoute(user, configuredRootDomain) : null);
  }, [configuredRootDomain, routes, user]);
  const catchAllRoute = useMemo(() => routes.find((item) => item.kind === 'catch_all') ?? null, [routes]);
  const customRoutes = useMemo(() => routes.filter((item) => item.kind === 'custom'), [routes]);
  const tableRows = useMemo(() => {
    const nextRows: UserEmailRoute[] = [];
    if (defaultRoute) nextRows.push(defaultRoute);
    nextRows.push(...customRoutes);
    if (catchAllRoute) nextRows.push(catchAllRoute);
    return nextRows;
  }, [catchAllRoute, customRoutes, defaultRoute]);

  const verifiedTargets = useMemo(
    () => emailTargets.filter((item) => item.verified),
    [emailTargets],
  );
  const selectableTargetOptions = useMemo<GlassSelectOption[]>(() => {
    const options = verifiedTargets.map((item) => ({
      value: item.email,
      label: item.email,
    }));
    return [{ value: '', label: '不转发 / 清空目标' }, ...options];
  }, [verifiedTargets]);

  const defaultAddress = defaultRoute?.address ?? (normalizedUsername ? `${normalizedUsername}@${configuredRootDomain}` : '');
  const catchAllAddress = useMemo(() => {
    if (catchAllRoute?.address) return catchAllRoute.address;
    if (permission?.target?.trim()) return permission.target.trim();
    return normalizedUsername ? `catch-all@${normalizedUsername}.${configuredRootDomain}` : `catch-all@<用户名>.${configuredRootDomain}`;
  }, [catchAllRoute?.address, configuredRootDomain, normalizedUsername, permission?.target]);
  const searchRootDomain = defaultRoute?.root_domain ?? searchResult?.root_domain ?? configuredRootDomain;
  const pledgeText = permission?.pledge_text?.trim() ?? '';
  const pendingTargetCount = emailTargets.length - verifiedTargets.length;
  const defaultTargetNeedsVerification = useMemo(
    () => routeTargetNeedsVerification(defaultRoute?.target_email, verifiedTargets),
    [defaultRoute?.target_email, verifiedTargets],
  );
  const catchAllTargetNeedsVerification = useMemo(
    () => routeTargetNeedsVerification(catchAllRoute?.target_email, verifiedTargets),
    [catchAllRoute?.target_email, verifiedTargets],
  );

  useEffect(() => {
    if (!authenticated) {
      loadRequestTokenRef.current += 1;
      searchRequestTokenRef.current += 1;
      setPermission(null);
      setRoutes([]);
      setEmailTargets([]);
      setLoading(false);
      setPermissionError('');
      setRouteError('');
      setTargetError('');
      setDefaultTarget('');
      setDefaultEnabled(false);
      setCatchAllTarget('');
      setCatchAllEnabled(false);
      setNewTargetEmail('');
      setDefaultNotice(null);
      setCatchAllNotice(null);
      setTargetNotice(null);
      setPledgeModalOpen(false);
      return;
    }

    void loadAuthenticatedData();
  }, [authenticated, user?.id]);

  useEffect(() => {
    setDefaultTarget(defaultRoute?.target_email ?? '');
    setDefaultEnabled(defaultRoute?.enabled ?? false);
  }, [defaultRoute?.address, defaultRoute?.target_email, defaultRoute?.enabled]);

  useEffect(() => {
    setCatchAllTarget(catchAllRoute?.target_email ?? '');
    setCatchAllEnabled(catchAllRoute?.enabled ?? false);
  }, [catchAllRoute?.address, catchAllRoute?.target_email, catchAllRoute?.enabled]);

  async function loadAuthenticatedData(): Promise<void> {
    const requestToken = ++loadRequestTokenRef.current;
    setLoading(true);
    setPermissionError('');
    setRouteError('');
    setTargetError('');

    const [permissionResult, routeResult, targetResult] = await Promise.allSettled([
      listMyPermissions(),
      listMyEmailRoutes(),
      listMyEmailTargets(),
    ]);
    if (requestToken !== loadRequestTokenRef.current) {
      return;
    }

    if (permissionResult.status === 'fulfilled') {
      setPermission(permissionResult.value.find((item) => item.key === catchAllPermissionKey) ?? null);
      setPermissionError('');
    } else {
      const maybePermissionError = permissionResult.reason;
      if (maybePermissionError instanceof APIError && maybePermissionError.code === 'not_found') {
        setPermission(null);
        setPermissionError('');
      } else {
        setPermission(null);
        setPermissionError(readableErrorMessage(permissionResult.reason, '无法加载邮箱泛解析权限数据。'));
      }
    }

    if (routeResult.status === 'fulfilled') {
      setRoutes(routeResult.value);
      setRouteError('');
    } else {
      setRoutes([]);
      setRouteError(readableErrorMessage(routeResult.reason, '无法加载我的邮箱列表。'));
    }

    if (targetResult.status === 'fulfilled') {
      setEmailTargets(targetResult.value);
      setTargetError('');
    } else {
      const maybeTargetError = targetResult.reason;
      if (maybeTargetError instanceof APIError && maybeTargetError.code === 'not_found') {
        setEmailTargets([]);
        setTargetError('');
      } else {
        setEmailTargets([]);
        setTargetError(readableErrorMessage(targetResult.reason, '无法加载我的转发目标列表。'));
      }
    }

    if (requestToken === loadRequestTokenRef.current) {
      setLoading(false);
    }
  }

  async function handleSearch(event: FormEvent<HTMLFormElement>): Promise<void> {
    event.preventDefault();
    const normalizedPrefix = normalizePrefix(searchPrefix);
    const requestToken = ++searchRequestTokenRef.current;
    if (!normalizedPrefix) {
      setSearchResult(null);
      setSearchStatus('error');
      setSearchMessage('请输入合法的邮箱前缀，只支持字母、数字和连字符。');
      return;
    }

    try {
      setSearchStatus('checking');
      setSearchResult(null);
      setSearchMessage('');
      setSearchPrefix(normalizedPrefix);
      const result = await checkPublicEmailRouteAvailability(searchRootDomain, normalizedPrefix);
      if (requestToken !== searchRequestTokenRef.current) {
        return;
      }
      setSearchResult(result);
      setSearchStatus(result.available ? 'available' : 'taken');
      setSearchMessage(buildSearchMessage(result, normalizedUsername, authenticated));
    } catch (error) {
      if (requestToken !== searchRequestTokenRef.current) {
        return;
      }
      setSearchResult(null);
      setSearchStatus('error');
      setSearchMessage(readableErrorMessage(error, '邮箱查询失败，请稍后重试。'));
    }
  }

  async function handleCreateTarget(event: FormEvent<HTMLFormElement>): Promise<void> {
    event.preventDefault();
    if (!csrfToken) {
      setTargetNotice({ tone: 'error', message: '当前会话缺少 CSRF Token，请重新登录后再试。' });
      return;
    }

    const nextEmail = normalizeEmail(newTargetEmail);
    if (!nextEmail) {
      setTargetNotice({ tone: 'error', message: '请输入要绑定的目标邮箱。' });
      return;
    }

    try {
      setCreatingTarget(true);
      setTargetNotice(null);
      const createdTarget = await createMyEmailTarget({ email: nextEmail }, csrfToken);
      setEmailTargets((currentTargets) => upsertEmailTarget(currentTargets, createdTarget));
      setNewTargetEmail('');
      setTargetNotice({
        tone: 'success',
        message: createdTarget.verified
          ? `目标邮箱 ${createdTarget.email} 已完成绑定，现在可以直接用于转发。`
          : `目标邮箱 ${createdTarget.email} 已绑定到你的账号。Cloudflare 验证邮件已发出，请前往邮箱确认后点击“刷新状态”。`,
      });
    } catch (error) {
      setTargetNotice({ tone: 'error', message: readableErrorMessage(error, '添加目标邮箱失败。') });
    } finally {
      setCreatingTarget(false);
    }
  }

  async function handleRefreshTargets(): Promise<void> {
    await loadAuthenticatedData();
    setTargetNotice({
      tone: 'info',
      message: '已刷新目标邮箱状态。若你刚完成邮箱确认，现在应该能看到最新验证结果。',
    });
  }

  async function handleSaveDefault(event: FormEvent<HTMLFormElement>): Promise<void> {
    event.preventDefault();
    if (!csrfToken) {
      setDefaultNotice({ tone: 'error', message: '当前会话缺少 CSRF Token，请重新登录后再试。' });
      return;
    }

    const nextTarget = normalizeEmail(defaultTarget);
    if (defaultEnabled && !nextTarget) {
      setDefaultNotice({ tone: 'error', message: '启用默认邮箱转发前，请先选择一个已验证的目标邮箱。' });
      return;
    }
    if (nextTarget && !isVerifiedTargetOwned(nextTarget, verifiedTargets)) {
      setDefaultNotice({ tone: 'error', message: '当前只能选择已经绑定到你账号且已完成 Cloudflare 验证的目标邮箱。' });
      return;
    }

    try {
      setSavingDefault(true);
      setDefaultNotice(null);
      const savedRoute = await upsertDefaultEmailRoute(
        { target_email: nextTarget, enabled: nextTarget !== '' ? defaultEnabled : false },
        csrfToken,
      );
      setRoutes((currentRoutes) => upsertRoute(currentRoutes, savedRoute));
      setDefaultNotice({
        tone: 'success',
        message: savedRoute.configured ? '默认邮箱已更新。' : '默认邮箱已清空，当前不会继续转发邮件。',
      });
    } catch (error) {
      setDefaultNotice({ tone: 'error', message: readableErrorMessage(error, '保存默认邮箱失败。') });
    } finally {
      setSavingDefault(false);
    }
  }

  async function handleApplyCatchAllPermission(): Promise<void> {
    if (!csrfToken || !permission?.can_apply) return;

    try {
      setApplyingPermission(true);
      setCatchAllNotice(null);
      const nextPermission = await submitPermissionApplication({ key: catchAllPermissionKey }, csrfToken);
      setPermission(nextPermission);
      setPledgeModalOpen(false);
      setCatchAllNotice({
        tone: 'success',
        message: nextPermission.status === 'approved'
          ? '邮箱泛解析权限申请已记录并自动通过，现在可以继续配置邮箱转发。'
          : '邮箱泛解析权限申请已提交，等待管理员处理。',
      });
    } catch (error) {
      setCatchAllNotice({ tone: 'error', message: readableErrorMessage(error, '提交邮箱泛解析权限申请失败。') });
    } finally {
      setApplyingPermission(false);
    }
  }

  async function handleSaveCatchAll(event: FormEvent<HTMLFormElement>): Promise<void> {
    event.preventDefault();
    if (!csrfToken) {
      setCatchAllNotice({ tone: 'error', message: '当前会话缺少 CSRF Token，请重新登录后再试。' });
      return;
    }
    if (!permission?.can_manage_route) {
      setCatchAllNotice({ tone: 'error', message: '当前尚未获得邮箱泛解析配置权限。' });
      return;
    }

    const nextTarget = normalizeEmail(catchAllTarget);
    if (catchAllEnabled && !nextTarget) {
      setCatchAllNotice({ tone: 'error', message: '启用邮箱泛解析转发前，请先选择一个已验证的目标邮箱。' });
      return;
    }
    if (nextTarget && !isVerifiedTargetOwned(nextTarget, verifiedTargets)) {
      setCatchAllNotice({ tone: 'error', message: '当前只能选择已经绑定到你账号且已完成 Cloudflare 验证的目标邮箱。' });
      return;
    }

    try {
      setSavingCatchAll(true);
      setCatchAllNotice(null);
      const savedRoute = await upsertCatchAllEmailRoute(
        { target_email: nextTarget, enabled: nextTarget !== '' ? catchAllEnabled : false },
        csrfToken,
      );
      setRoutes((currentRoutes) => upsertRoute(currentRoutes, savedRoute));
      setCatchAllNotice({
        tone: 'success',
        message: savedRoute.configured ? '邮箱泛解析已更新。' : '邮箱泛解析已清空，当前不会继续转发邮件。',
      });
    } catch (error) {
      setCatchAllNotice({ tone: 'error', message: readableErrorMessage(error, '保存邮箱泛解析失败。') });
    } finally {
      setSavingCatchAll(false);
    }
  }

  return (
    <div className="mx-auto max-w-6xl px-6 pb-24 pt-32">
      <motion.div initial={{ y: 20, opacity: 0 }} animate={{ y: 0, opacity: 1 }} className="mb-8 flex flex-col gap-6">
        <div className="text-center">
          <div className="inline-flex items-center gap-2 rounded-full border border-white/30 bg-white/35 px-4 py-2 text-sm font-semibold text-teal-700 backdrop-blur-md dark:border-white/10 dark:bg-black/30 dark:text-teal-300">
            <Mail size={16} />
            邮箱分发
          </div>
          <h1 className="mt-5 text-4xl font-extrabold text-gray-900 dark:text-white md:text-5xl">搜索、保留并管理你的 LinuxDoSpace 邮箱</h1>
          <p className="mx-auto mt-4 max-w-4xl text-lg leading-relaxed text-gray-700 dark:text-gray-200">
            搜索功能对所有访客开放。登录后，你可以先绑定自己的转发目标邮箱，再管理默认邮箱
            <span className="font-semibold text-gray-900 dark:text-white"> {defaultAddress || '<用户名>@linuxdo.space'}</span>
            ，并在获得权限后配置
            <span className="font-semibold text-gray-900 dark:text-white"> {catchAllAddress}</span>。
          </p>
        </div>

        <div className="grid gap-6 xl:grid-cols-[1.2fr_0.8fr]">
          <GlassCard className="overflow-hidden p-0">
            <div className="border-b border-white/15 bg-white/15 px-6 py-5 dark:border-white/10 dark:bg-black/10">
              <div className="flex items-center gap-3">
                <div className="rounded-2xl bg-sky-500/15 p-3 text-sky-700 dark:text-sky-300"><Search size={20} /></div>
                <div>
                  <h2 className="text-xl font-bold text-gray-900 dark:text-white">邮箱搜索</h2>
                  <p className="mt-1 text-sm text-gray-600 dark:text-gray-300">查询某个邮箱前缀是否已被占用。搜索始终开放，但普通自定义邮箱注册入口暂未开放。</p>
                </div>
              </div>
            </div>

            <div className="space-y-5 p-6">
              <form className="space-y-4" onSubmit={(event) => void handleSearch(event)}>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300" htmlFor="email-prefix-search">邮箱前缀</label>
                <div className="flex flex-col gap-3 md:flex-row">
                  <div className="flex min-w-0 flex-1 items-center rounded-2xl border border-white/20 bg-white/55 px-4 py-3 shadow-inner dark:border-white/10 dark:bg-black/35">
                    <input
                      id="email-prefix-search"
                      type="text"
                      value={searchPrefix}
                      onChange={(event) => setSearchPrefix(event.target.value)}
                      placeholder="例如 alice"
                      className="min-w-0 flex-1 bg-transparent text-base text-gray-900 outline-none placeholder:text-gray-400 dark:text-white dark:placeholder:text-gray-500"
                    />
                    <span className="ml-3 shrink-0 text-sm font-medium text-gray-500 dark:text-gray-400">@{searchRootDomain}</span>
                  </div>
                  <button
                    type="submit"
                    disabled={searchStatus === 'checking'}
                    className="inline-flex items-center justify-center gap-2 rounded-2xl bg-gradient-to-r from-sky-500 to-cyan-500 px-5 py-3 font-semibold text-white shadow-lg transition hover:from-sky-600 hover:to-cyan-600 disabled:cursor-not-allowed disabled:opacity-60"
                  >
                    {searchStatus === 'checking' ? <LoaderCircle className="animate-spin" size={18} /> : <Search size={18} />}
                    立即搜索
                  </button>
                </div>
              </form>

              <div className="rounded-3xl border border-white/20 bg-white/45 p-5 dark:border-white/10 dark:bg-black/30">
                <div className="flex flex-wrap items-center gap-3">
                  <StatusChip {...describeSearchStatus(searchStatus)} />
                  {searchResult?.address ? <span className="rounded-full bg-white/70 px-3 py-1 text-sm font-medium text-gray-700 dark:bg-black/35 dark:text-gray-200">{searchResult.address}</span> : null}
                </div>
                <p className="mt-4 text-sm leading-7 text-gray-700 dark:text-gray-200">{searchMessage || '输入邮箱前缀后即可查询。若前缀与你的用户名一致，登录后可直接在下方管理默认邮箱。'}</p>
              </div>
            </div>
          </GlassCard>

          <GlassCard className="space-y-4">
            <div className="flex items-center gap-3">
              <div className="rounded-2xl bg-emerald-500/15 p-3 text-emerald-700 dark:text-emerald-300"><ShieldCheck size={20} /></div>
              <div>
                <h2 className="text-xl font-bold text-gray-900 dark:text-white">使用说明</h2>
                <p className="mt-1 text-sm text-gray-600 dark:text-gray-300">邮箱能力分成公开搜索、目标邮箱绑定、默认邮箱和权限邮箱四部分。</p>
              </div>
            </div>

            <InfoBlock title="默认邮箱" description={normalizedUsername ? `每位用户默认保留 ${normalizedUsername}@${configuredRootDomain}，但必须先绑定自己的目标邮箱后才能转发。` : '每位用户都会默认保留一个与用户名同名的邮箱地址。'} />
            <InfoBlock title="我的转发目标" description="先在“我的转发目标”里绑定目标邮箱。新增后 Cloudflare 会向该邮箱发送确认邮件，验证完成后该目标才会出现在下拉选择器里。" />
            <InfoBlock title="我的邮箱列表" description="这里会展示当前账号已经存在或默认保留的邮箱行，包括默认邮箱、已存在的自定义邮箱以及权限邮箱 catch-all。" />
            <InfoBlock title="邮箱泛解析权限" description="catch-all@<用户名>.linuxdo.space 不是默认开放功能。只有满足权限条件的用户才可以申请，并在通过后配置转发目标。" />
          </GlassCard>
        </div>
      </motion.div>

      {!authenticated ? (
        <GlassCard className="space-y-4">
          <div className="flex items-start gap-3">
            <div className="rounded-2xl bg-amber-500/15 p-3 text-amber-700 dark:text-amber-300">
              {sessionLoading ? <LoaderCircle className="animate-spin" size={20} /> : <Mail size={20} />}
            </div>
            <div>
              <h2 className="text-xl font-bold text-gray-900 dark:text-white">{sessionLoading ? '正在检查登录状态' : '登录后管理我的邮箱'}</h2>
              <p className="mt-2 text-sm leading-7 text-gray-700 dark:text-gray-200">
                {sessionLoading
                  ? '你现在仍可使用上方搜索功能。待登录状态加载完成后，再进入目标邮箱绑定、默认邮箱和 catch-all 邮箱配置。'
                  : '搜索功能无需登录，但目标邮箱绑定、默认邮箱配置、我的邮箱列表和 catch-all 邮箱权限申请都需要使用 Linux Do 账号登录。'}
              </p>
            </div>
          </div>

          {!sessionLoading ? (
            <button
              type="button"
              onClick={onLogin}
              className="inline-flex items-center gap-2 rounded-2xl bg-gradient-to-r from-teal-500 to-emerald-600 px-5 py-3 font-semibold text-white shadow-lg transition hover:from-teal-600 hover:to-emerald-700"
            >
              使用 Linux Do 登录
              <ArrowRight size={18} />
            </button>
          ) : null}
        </GlassCard>
      ) : (
        <div className="space-y-6">
          <GlassCard className="overflow-hidden p-0">
            <div className="flex flex-col gap-4 border-b border-white/15 bg-white/15 px-6 py-5 dark:border-white/10 dark:bg-black/10 md:flex-row md:items-center md:justify-between">
              <div>
                <h2 className="text-xl font-bold text-gray-900 dark:text-white">我的邮箱列表</h2>
                <p className="mt-1 text-sm text-gray-600 dark:text-gray-300">展示当前账号已保留或已配置的邮箱。默认邮箱始终展示，便于你直接开始配置。</p>
              </div>
              <div className="inline-flex items-center gap-2 rounded-full bg-white/60 px-4 py-2 text-sm font-medium text-gray-700 dark:bg-black/30 dark:text-gray-200">
                {loading ? <LoaderCircle className="animate-spin" size={16} /> : <Mail size={16} />}
                共 {tableRows.length} 项
              </div>
            </div>

            <div className="space-y-4 p-6">
              {routeError ? <InlineNotice tone="error" message={`邮箱列表加载失败：${routeError}`} /> : null}

              <div className="overflow-x-auto rounded-3xl border border-white/15 bg-white/35 dark:border-white/10 dark:bg-black/20">
                <table className="w-full min-w-[720px] border-collapse text-left">
                  <thead>
                    <tr className="border-b border-white/15 text-sm text-gray-600 dark:border-white/10 dark:text-gray-300">
                      <th className="px-5 py-4 font-semibold">邮箱地址</th>
                      <th className="px-5 py-4 font-semibold">类型</th>
                      <th className="px-5 py-4 font-semibold">转发目标</th>
                      <th className="px-5 py-4 font-semibold">状态</th>
                      <th className="px-5 py-4 font-semibold">更新时间</th>
                    </tr>
                  </thead>
                  <tbody>
                    {tableRows.map((route) => {
                      const status = describeRouteStatus(route);
                      return (
                        <tr key={`${route.kind}:${route.address}:${route.id ?? 'implicit'}`} className="border-b border-white/10 last:border-b-0 hover:bg-white/30 dark:border-white/5 dark:hover:bg-white/5">
                          <td className="px-5 py-4 align-top">
                            <div className="font-semibold text-gray-900 dark:text-white">{route.address}</div>
                            <div className="mt-1 text-sm text-gray-500 dark:text-gray-400">{route.description}</div>
                          </td>
                          <td className="px-5 py-4 align-top text-sm text-gray-700 dark:text-gray-200">{describeRouteKind(route.kind)}</td>
                          <td className="px-5 py-4 align-top text-sm text-gray-700 dark:text-gray-200">{route.target_email || '尚未设置转发目标'}</td>
                          <td className="px-5 py-4 align-top"><StatusChip {...status} /></td>
                          <td className="px-5 py-4 align-top text-sm text-gray-600 dark:text-gray-300">{formatDate(route.updated_at)}</td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            </div>
          </GlassCard>

          <GlassCard className="space-y-5">
            <div className="flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
              <div className="flex items-center gap-3">
                <div className="rounded-2xl bg-sky-500/15 p-3 text-sky-700 dark:text-sky-300"><Mail size={20} /></div>
                <div>
                  <h2 className="text-xl font-bold text-gray-900 dark:text-white">我的转发目标</h2>
                  <p className="mt-1 text-sm text-gray-600 dark:text-gray-300">先绑定并验证目标邮箱，再把它用于默认邮箱或 catch-all 权限邮箱转发。</p>
                </div>
              </div>

              <button
                type="button"
                onClick={() => void handleRefreshTargets()}
                disabled={loading}
                className="inline-flex items-center gap-2 rounded-2xl border border-white/20 bg-white/60 px-4 py-3 font-medium text-gray-900 transition hover:bg-white/80 disabled:cursor-not-allowed disabled:opacity-60 dark:border-white/10 dark:bg-black/30 dark:text-white dark:hover:bg-black/45"
              >
                {loading ? <LoaderCircle className="animate-spin" size={16} /> : <RefreshCw size={16} />}
                刷新状态
              </button>
            </div>

            {targetError ? <InlineNotice tone="error" message={`目标邮箱列表加载失败：${targetError}`} /> : null}
            {targetNotice ? <InlineNotice tone={targetNotice.tone} message={targetNotice.message} /> : null}

            <div className="grid gap-3 md:grid-cols-3">
              <InfoStat title="已绑定目标" value={`${emailTargets.length} 个`} />
              <InfoStat title="已验证" value={`${verifiedTargets.length} 个`} />
              <InfoStat title="待确认" value={`${pendingTargetCount} 个`} />
            </div>

            <div className="rounded-2xl border border-white/15 bg-white/35 p-4 text-sm leading-7 text-gray-700 dark:border-white/10 dark:bg-black/20 dark:text-gray-200">
              每个目标邮箱都会和当前 LinuxDoSpace 账号绑定，其他用户不能重复占用。首次添加时，Cloudflare 会向该邮箱发送确认邮件；只有完成确认后，这个目标邮箱才会出现在下方配置下拉框中。
            </div>

            <form className="space-y-4" onSubmit={(event) => void handleCreateTarget(event)}>
              <div className="grid gap-3 lg:grid-cols-[1fr_auto]">
                <div className="flex min-w-0 items-center rounded-2xl border border-white/20 bg-white/55 px-4 py-3 shadow-inner dark:border-white/10 dark:bg-black/35">
                  <input
                    type="email"
                    value={newTargetEmail}
                    onChange={(event) => setNewTargetEmail(event.target.value)}
                    placeholder="例如 you@example.com"
                    className="min-w-0 flex-1 bg-transparent text-base text-gray-900 outline-none placeholder:text-gray-400 dark:text-white dark:placeholder:text-gray-500"
                  />
                </div>
                <button
                  type="submit"
                  disabled={creatingTarget}
                  className="inline-flex items-center justify-center gap-2 rounded-2xl bg-gradient-to-r from-sky-500 to-cyan-500 px-5 py-3 font-semibold text-white shadow-lg transition hover:from-sky-600 hover:to-cyan-600 disabled:cursor-not-allowed disabled:opacity-60"
                >
                  {creatingTarget ? <LoaderCircle className="animate-spin" size={18} /> : <Plus size={18} />}
                  添加目标邮箱
                </button>
              </div>
            </form>

            {emailTargets.length === 0 ? (
              <div className="rounded-3xl border border-dashed border-white/20 bg-white/25 p-6 text-sm leading-7 text-gray-700 dark:border-white/10 dark:bg-black/15 dark:text-gray-200">
                你当前还没有绑定任何目标邮箱。先添加一个你自己的邮箱地址，完成 Cloudflare 验证后，它才会出现在默认邮箱和 catch-all 权限邮箱的下拉框里。
              </div>
            ) : (
              <div className="overflow-x-auto rounded-3xl border border-white/15 bg-white/35 dark:border-white/10 dark:bg-black/20">
                <table className="w-full min-w-[760px] border-collapse text-left">
                  <thead>
                    <tr className="border-b border-white/15 text-sm text-gray-600 dark:border-white/10 dark:text-gray-300">
                      <th className="px-5 py-4 font-semibold">目标邮箱</th>
                      <th className="px-5 py-4 font-semibold">验证状态</th>
                      <th className="px-5 py-4 font-semibold">同步状态</th>
                      <th className="px-5 py-4 font-semibold">最近动作</th>
                    </tr>
                  </thead>
                  <tbody>
                    {emailTargets.map((item) => {
                      const status = describeEmailTargetStatus(item);
                      return (
                        <tr key={item.id} className="border-b border-white/10 last:border-b-0 hover:bg-white/30 dark:border-white/5 dark:hover:bg-white/5">
                          <td className="px-5 py-4 align-top">
                            <div className="font-semibold text-gray-900 dark:text-white">{item.email}</div>
                            <div className="mt-1 text-sm text-gray-500 dark:text-gray-400">只允许当前账号再次使用这个目标邮箱。</div>
                          </td>
                          <td className="px-5 py-4 align-top">
                            <StatusChip {...status} />
                            <div className="mt-2 text-sm text-gray-600 dark:text-gray-300">
                              {item.verified_at ? `验证通过：${formatDate(item.verified_at)}` : '等待你在目标邮箱中确认 Cloudflare 验证邮件。'}
                            </div>
                          </td>
                          <td className="px-5 py-4 align-top text-sm text-gray-700 dark:text-gray-200">
                            {item.cloudflare_address_id ? '已在 Cloudflare 建立目标邮箱绑定' : '等待 Cloudflare 创建目标邮箱绑定'}
                          </td>
                          <td className="px-5 py-4 align-top text-sm text-gray-600 dark:text-gray-300">
                            {item.last_verification_sent_at ? `验证邮件发送于 ${formatDate(item.last_verification_sent_at)}` : `最近更新于 ${formatDate(item.updated_at)}`}
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            )}
          </GlassCard>

          <div className="grid gap-6 xl:grid-cols-2">
            <GlassCard className="space-y-5">
              <div className="flex items-center gap-3">
                <div className="rounded-2xl bg-teal-500/15 p-3 text-teal-700 dark:text-teal-300"><Mail size={20} /></div>
                <div>
                  <h2 className="text-xl font-bold text-gray-900 dark:text-white">默认邮箱设置</h2>
                  <p className="mt-1 text-sm text-gray-600 dark:text-gray-300">默认邮箱始终为 <span className="font-semibold">{defaultAddress || '<用户名>@linuxdo.space'}</span>。</p>
                </div>
              </div>

              {defaultNotice ? <InlineNotice tone={defaultNotice.tone} message={defaultNotice.message} /> : null}
              {defaultTargetNeedsVerification && defaultRoute?.target_email ? (
                <InlineNotice tone="info" message={`当前已保存的目标邮箱 ${defaultRoute.target_email} 尚未完成验证。完成验证后刷新状态，或直接改选其他已验证目标邮箱。`} />
              ) : null}

              <form className="space-y-4" onSubmit={(event) => void handleSaveDefault(event)}>
                <div className="space-y-2">
                  <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">已验证的转发目标</label>
                  <GlassSelect
                    options={selectableTargetOptions}
                    value={defaultTarget}
                    onChange={setDefaultTarget}
                    placeholder={verifiedTargets.length > 0 ? '请选择已验证的目标邮箱' : '暂无已验证的目标邮箱'}
                    disabled={savingDefault}
                  />
                  <div className="text-sm leading-7 text-gray-600 dark:text-gray-300">只有上方“我的转发目标”中已经完成 Cloudflare 验证的邮箱，才允许被保存为默认邮箱转发目标。</div>
                </div>

                <ToggleSwitch
                  title="启用默认邮箱转发"
                  description="关闭后会保留邮箱地址，但暂时不再转发邮件。"
                  checked={defaultEnabled}
                  onCheckedChange={setDefaultEnabled}
                  disabled={savingDefault}
                />

                <div className="rounded-2xl border border-white/15 bg-white/35 p-4 text-sm leading-7 text-gray-700 dark:border-white/10 dark:bg-black/20 dark:text-gray-200">
                  每个用户都会自动保留一个与用户名同名的邮箱地址。你可以选择已验证的目标邮箱进行转发，也可以直接清空目标来停用转发。
                </div>

                <button
                  type="submit"
                  disabled={savingDefault}
                  className="inline-flex items-center gap-2 rounded-2xl bg-gradient-to-r from-teal-500 to-emerald-600 px-5 py-3 font-semibold text-white shadow-lg transition hover:from-teal-600 hover:to-emerald-700 disabled:cursor-not-allowed disabled:opacity-60"
                >
                  {savingDefault ? <LoaderCircle className="animate-spin" size={18} /> : <Sparkles size={18} />}
                  保存默认邮箱
                </button>
              </form>
            </GlassCard>

            <GlassCard className="space-y-5">
              <div className="flex items-center gap-3">
                <div className="rounded-2xl bg-violet-500/15 p-3 text-violet-700 dark:text-violet-300"><ShieldCheck size={20} /></div>
                <div>
                  <h2 className="text-xl font-bold text-gray-900 dark:text-white">邮箱泛解析权限与配置</h2>
                  <p className="mt-1 text-sm text-gray-600 dark:text-gray-300">当前目标地址：<span className="font-semibold">{catchAllAddress}</span></p>
                </div>
              </div>

              {permissionError ? <InlineNotice tone="error" message={`权限数据加载失败：${permissionError}`} /> : null}
              {catchAllNotice ? <InlineNotice tone={catchAllNotice.tone} message={catchAllNotice.message} /> : null}
              {catchAllTargetNeedsVerification && catchAllRoute?.target_email ? (
                <InlineNotice tone="info" message={`当前已保存的邮箱泛解析目标邮箱 ${catchAllRoute.target_email} 尚未完成验证。完成验证后刷新状态，或改选其他已验证目标邮箱。`} />
              ) : null}
              {!permission && !permissionError ? <InlineNotice tone="info" message="当前后端没有返回邮箱泛解析权限配置，因此暂时无法申请或管理此功能。" /> : null}

              {permission ? (
                <>
                  <div className="grid gap-3 md:grid-cols-2">
                    <InfoStat title="权限状态" value={describePermissionStatus(permission.status).label} />
                    <InfoStat title="自动审批" value={permission.auto_approve ? '是' : '否'} />
                    <InfoStat title="最低等级要求" value={`TL ${permission.min_trust_level}`} />
                    <InfoStat title="当前等级" value={user ? `TL ${user.trust_level}` : '未登录'} />
                  </div>

                  <div className="rounded-2xl border border-white/15 bg-white/35 p-4 dark:border-white/10 dark:bg-black/20">
                    <div className="flex flex-wrap items-center gap-3">
                      <StatusChip {...describePermissionStatus(permission.status)} />
                      <StatusChip
                        label={permission.eligible ? '当前满足申请条件' : '当前不满足申请条件'}
                        className={permission.eligible ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/25 dark:text-emerald-300' : 'bg-amber-100 text-amber-700 dark:bg-amber-900/25 dark:text-amber-300'}
                      />
                    </div>
                    <p className="mt-4 text-sm leading-7 text-gray-700 dark:text-gray-200">{permission.description}</p>
                    {permission.eligibility_reasons.length > 0 ? <div className="mt-4 rounded-2xl border border-amber-300/35 bg-amber-100/60 p-4 text-sm leading-7 text-amber-900 dark:border-amber-700/35 dark:bg-amber-950/25 dark:text-amber-100">{permission.eligibility_reasons.map((reason) => formatEligibilityReason(reason, permission)).join('；')}</div> : null}
                    {permission.application ? <div className="mt-4 text-sm text-gray-600 dark:text-gray-300">最近一次申请时间：{formatDate(permission.application.created_at)}</div> : null}
                  </div>

                  <div className="rounded-2xl border border-white/15 bg-white/35 p-4 dark:border-white/10 dark:bg-black/20">
                    <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
                      <div>
                        <div className="text-sm font-semibold text-gray-900 dark:text-white">承诺书</div>
                        <div className="mt-1 text-sm text-gray-600 dark:text-gray-300">{pledgeText ? '点击按钮以弹窗形式查看承诺书，并在确认后提交申请。' : '当前无承诺书。你仍然可以打开弹窗查看空状态提示。'}</div>
                      </div>
                      <button
                        type="button"
                        onClick={() => setPledgeModalOpen(true)}
                        className="inline-flex min-w-[10rem] shrink-0 items-center justify-center gap-2 whitespace-nowrap rounded-2xl border border-white/20 bg-white/60 px-4 py-2.5 font-medium text-gray-900 transition hover:bg-white/80 dark:border-white/10 dark:bg-black/30 dark:text-white dark:hover:bg-black/45"
                      >
                        查看承诺书
                        <ArrowRight size={16} />
                      </button>
                    </div>
                  </div>

                  <form className="space-y-4" onSubmit={(event) => void handleSaveCatchAll(event)}>
                    <div className="space-y-2">
                      <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">已验证的转发目标</label>
                      <GlassSelect
                        options={selectableTargetOptions}
                        value={catchAllTarget}
                        onChange={setCatchAllTarget}
                        placeholder={verifiedTargets.length > 0 ? '请选择已验证的目标邮箱' : '暂无已验证的目标邮箱'}
                        disabled={!permission.can_manage_route || savingCatchAll}
                      />
                      <div className="text-sm leading-7 text-gray-600 dark:text-gray-300">邮箱泛解析只允许使用已经绑定到你账号并完成 Cloudflare 验证的目标邮箱。</div>
                    </div>

                    <ToggleSwitch
                      title="启用邮箱泛解析转发"
                      description="关闭后会保留权限与配置入口，但暂时不再转发 catch-all 专用地址收到的邮件。"
                      checked={catchAllEnabled}
                      onCheckedChange={setCatchAllEnabled}
                      disabled={!permission.can_manage_route || savingCatchAll}
                    />

                    <div className="rounded-2xl border border-white/15 bg-white/35 p-4 text-sm leading-7 text-gray-700 dark:border-white/10 dark:bg-black/20 dark:text-gray-200">
                      {permission.can_manage_route
                        ? `你已经拥有配置权限，现在可以从已验证的目标邮箱中选择一个，用于 ${catchAllAddress} 的转发。`
                        : permission.can_apply
                          ? `你还没有该权限。请先阅读承诺书并提交申请，申请通过后才能配置 ${catchAllAddress} 的转发。`
                          : '当前不能直接管理该权限邮箱。若状态为待审核或未通过，请等待管理员处理。'}
                    </div>

                    <div className="flex flex-wrap gap-3">
                      <button
                        type="submit"
                        disabled={!permission.can_manage_route || savingCatchAll}
                        className="inline-flex items-center gap-2 rounded-2xl bg-gradient-to-r from-violet-500 to-fuchsia-600 px-5 py-3 font-semibold text-white shadow-lg transition hover:from-violet-600 hover:to-fuchsia-700 disabled:cursor-not-allowed disabled:opacity-60"
                      >
                        {savingCatchAll ? <LoaderCircle className="animate-spin" size={18} /> : <Sparkles size={18} />}
                        保存 catch-all 邮箱配置
                      </button>

                      {permission.can_apply ? (
                        <button
                          type="button"
                          onClick={() => setPledgeModalOpen(true)}
                          className="inline-flex items-center gap-2 rounded-2xl border border-white/20 bg-white/60 px-4 py-3 font-medium text-gray-900 transition hover:bg-white/80 dark:border-white/10 dark:bg-black/30 dark:text-white dark:hover:bg-black/45"
                        >
                          申请 catch-all 邮箱权限
                          <ArrowRight size={16} />
                        </button>
                      ) : null}
                    </div>
                  </form>
                </>
              ) : null}
            </GlassCard>
          </div>
        </div>
      )}

      <AnimatePresence>
        {pledgeModalOpen && permission ? (
          <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="fixed inset-0 z-50 flex items-center justify-center bg-black/45 p-6 backdrop-blur-sm" onClick={() => setPledgeModalOpen(false)}>
            <motion.div
              initial={{ opacity: 0, y: 24, scale: 0.96 }}
              animate={{ opacity: 1, y: 0, scale: 1 }}
              exit={{ opacity: 0, y: 24, scale: 0.96 }}
              transition={{ duration: 0.2 }}
              className="w-full max-w-2xl rounded-[2rem] border border-white/20 bg-white/80 p-6 shadow-2xl backdrop-blur-2xl dark:border-white/10 dark:bg-slate-950/85"
              onClick={(event) => event.stopPropagation()}
            >
              <div className="flex items-start justify-between gap-4">
                <div>
                  <div className="inline-flex items-center gap-2 rounded-full bg-white/70 px-3 py-1 text-xs font-semibold text-slate-700 dark:bg-black/30 dark:text-slate-200"><ShieldCheck size={14} />邮箱泛解析权限承诺书</div>
                  <h3 className="mt-3 text-2xl font-bold text-gray-900 dark:text-white">{permission.display_name}</h3>
                  <p className="mt-2 text-sm text-gray-600 dark:text-gray-300">目标权限：{permission.target || catchAllAddress}</p>
                </div>
                <button type="button" onClick={() => setPledgeModalOpen(false)} className="rounded-2xl border border-white/20 bg-white/70 p-2 text-gray-700 transition hover:bg-white dark:border-white/10 dark:bg-black/35 dark:text-gray-200 dark:hover:bg-black/50"><X size={18} /></button>
              </div>

              <div className="mt-6 rounded-3xl border border-white/15 bg-white/45 p-5 dark:border-white/10 dark:bg-black/25">
                {pledgeText ? <div className="whitespace-pre-wrap text-sm leading-8 text-gray-700 dark:text-gray-200">{pledgeText}</div> : <div className="rounded-2xl border border-amber-300/35 bg-amber-100/65 p-4 text-sm leading-7 text-amber-900 dark:border-amber-700/35 dark:bg-amber-950/25 dark:text-amber-100">当前无承诺书。页面已明确区分“权限数据加载失败”和“当前无承诺书”，当前属于后者。</div>}
              </div>

              <div className="mt-6 flex flex-wrap justify-end gap-3">
                <button type="button" onClick={() => setPledgeModalOpen(false)} className="rounded-2xl border border-white/20 bg-white/70 px-4 py-3 font-medium text-gray-900 transition hover:bg-white dark:border-white/10 dark:bg-black/35 dark:text-white dark:hover:bg-black/50">关闭</button>
                <button
                  type="button"
                  disabled={!permission.can_apply || applyingPermission}
                  onClick={() => void handleApplyCatchAllPermission()}
                  className="inline-flex items-center gap-2 rounded-2xl bg-gradient-to-r from-sky-500 to-cyan-500 px-5 py-3 font-semibold text-white shadow-lg transition hover:from-sky-600 hover:to-cyan-600 disabled:cursor-not-allowed disabled:opacity-60"
                >
                  {applyingPermission ? <LoaderCircle className="animate-spin" size={18} /> : <Sparkles size={18} />}
                  {permission.can_apply ? '确认承诺并提交申请' : '当前不可提交申请'}
                </button>
              </div>
            </motion.div>
          </motion.div>
        ) : null}
      </AnimatePresence>
    </div>
  );
}

interface InfoBlockProps {
  title: string;
  description: string;
}

function InfoBlock({ title, description }: InfoBlockProps) {
  return (
    <div className="rounded-2xl border border-white/15 bg-white/35 p-4 dark:border-white/10 dark:bg-black/20">
      <div className="text-sm font-semibold text-gray-900 dark:text-white">{title}</div>
      <div className="mt-2 text-sm leading-7 text-gray-700 dark:text-gray-200">{description}</div>
    </div>
  );
}

interface InfoStatProps {
  title: string;
  value: string;
}

function InfoStat({ title, value }: InfoStatProps) {
  return (
    <div className="rounded-2xl border border-white/15 bg-white/35 p-4 dark:border-white/10 dark:bg-black/20">
      <div className="text-xs font-semibold uppercase tracking-[0.18em] text-gray-500 dark:text-gray-400">{title}</div>
      <div className="mt-2 text-base font-semibold text-gray-900 dark:text-white">{value}</div>
    </div>
  );
}

interface InlineNoticeProps {
  tone: NoticeTone;
  message: string;
}

function InlineNotice({ tone, message }: InlineNoticeProps) {
  const palette = describeNoticePalette(tone);
  const Icon = tone === 'success' ? CheckCircle2 : AlertCircle;

  return (
    <div className={`rounded-2xl border px-4 py-3 text-sm leading-7 ${palette}`}>
      <div className="flex items-start gap-3">
        <Icon className="mt-1 shrink-0" size={18} />
        <div>{message}</div>
      </div>
    </div>
  );
}

function StatusChip({ label, className }: ChipDescriptor) {
  return <span className={`inline-flex rounded-full px-3 py-1 text-sm font-semibold ${className}`}>{label}</span>;
}

function buildImplicitDefaultRoute(user: User, rootDomain: string): UserEmailRoute {
  const prefix = normalizePrefix(user.username);
  return {
    kind: 'default',
    display_name: '默认邮箱',
    description: '每位用户自动保留一个与用户名同名的邮箱地址。',
    address: `${prefix}@${rootDomain}`,
    prefix,
    root_domain: rootDomain,
    target_email: '',
    enabled: false,
    configured: false,
    can_manage: true,
    can_delete: false,
  };
}

function upsertRoute(routes: UserEmailRoute[], nextRoute: UserEmailRoute): UserEmailRoute[] {
  if (nextRoute.kind === 'custom' && nextRoute.id) {
    const customIndex = routes.findIndex((item) => item.kind === 'custom' && item.id === nextRoute.id);
    if (customIndex >= 0) {
      return routes.map((item, index) => (index === customIndex ? nextRoute : item));
    }
    return [...routes, nextRoute];
  }

  const filteredRoutes = routes.filter((item) => item.kind !== nextRoute.kind);
  if (nextRoute.kind === 'default') return [nextRoute, ...filteredRoutes];

  const currentDefaultRoute = filteredRoutes.find((item) => item.kind === 'default');
  const otherRoutes = filteredRoutes.filter((item) => item.kind !== 'default');
  return currentDefaultRoute ? [currentDefaultRoute, ...otherRoutes, nextRoute] : [...otherRoutes, nextRoute];
}

function upsertEmailTarget(items: UserEmailTarget[], nextItem: UserEmailTarget): UserEmailTarget[] {
  const existingIndex = items.findIndex((item) => item.id === nextItem.id);
  if (existingIndex >= 0) {
    return items.map((item, index) => (index === existingIndex ? nextItem : item));
  }
  return [...items, nextItem].sort((left, right) => {
    if (left.verified !== right.verified) {
      return left.verified ? -1 : 1;
    }
    return normalizeEmail(left.email).localeCompare(normalizeEmail(right.email));
  });
}

function describeSearchStatus(status: SearchStatus): ChipDescriptor {
  switch (status) {
    case 'checking':
      return { label: '正在查询', className: 'bg-sky-100 text-sky-700 dark:bg-sky-900/25 dark:text-sky-300' };
    case 'available':
      return { label: '当前可用', className: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/25 dark:text-emerald-300' };
    case 'taken':
      return { label: '已被占用', className: 'bg-amber-100 text-amber-700 dark:bg-amber-900/25 dark:text-amber-300' };
    case 'error':
      return { label: '查询失败', className: 'bg-red-100 text-red-700 dark:bg-red-900/25 dark:text-red-300' };
    default:
      return { label: '等待查询', className: 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300' };
  }
}

function describePermissionStatus(status: PermissionStatus): ChipDescriptor {
  switch (status) {
    case 'approved':
      return { label: '已通过', className: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/25 dark:text-emerald-300' };
    case 'pending':
      return { label: '待审核', className: 'bg-amber-100 text-amber-700 dark:bg-amber-900/25 dark:text-amber-300' };
    case 'rejected':
      return { label: '未通过', className: 'bg-red-100 text-red-700 dark:bg-red-900/25 dark:text-red-300' };
    default:
      return { label: '尚未申请', className: 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300' };
  }
}

function describeRouteStatus(route: UserEmailRoute): ChipDescriptor {
  if (route.kind === 'catch_all' && route.permission_status && route.permission_status !== 'approved') {
    return describePermissionStatus(route.permission_status);
  }
  if (!route.configured) {
    return { label: '未配置', className: 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300' };
  }
  if (!route.enabled) {
    return { label: '已停用', className: 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300' };
  }
  return { label: '已启用', className: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/25 dark:text-emerald-300' };
}

function describeEmailTargetStatus(target: UserEmailTarget): ChipDescriptor {
  if (target.verified) {
    return { label: '已验证', className: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/25 dark:text-emerald-300' };
  }
  return { label: '待验证', className: 'bg-amber-100 text-amber-700 dark:bg-amber-900/25 dark:text-amber-300' };
}

function describeRouteKind(kind: UserEmailRoute['kind']): string {
  switch (kind) {
    case 'default':
      return '默认邮箱';
    case 'catch_all':
      return 'catch-all 邮箱';
    default:
      return '自定义邮箱';
  }
}

function describeNoticePalette(tone: NoticeTone): string {
  switch (tone) {
    case 'success':
      return 'border-emerald-300/35 bg-emerald-100/70 text-emerald-900 dark:border-emerald-700/35 dark:bg-emerald-950/30 dark:text-emerald-100';
    case 'error':
      return 'border-red-300/35 bg-red-100/70 text-red-900 dark:border-red-700/35 dark:bg-red-950/30 dark:text-red-100';
    default:
      return 'border-sky-300/35 bg-sky-100/70 text-sky-900 dark:border-sky-700/35 dark:bg-sky-950/30 dark:text-sky-100';
  }
}

function buildSearchMessage(result: EmailRouteAvailabilityResult, normalizedUsername: string, authenticated: boolean): string {
  if (result.available) {
    if (authenticated && normalizedUsername && result.normalized_prefix === normalizedUsername) {
      return '这个前缀与你的用户名一致。登录后可直接在下方配置默认邮箱转发。';
    }
    return '当前邮箱前缀未被占用。搜索功能公开开放，但新的普通邮箱注册入口暂未开放。';
  }

  if (authenticated && normalizedUsername && result.normalized_prefix === normalizedUsername) {
    return '这是你的默认邮箱地址，查询结果显示已被占用属于正常现象。';
  }
  if (result.reasons.includes('reserved_by_existing_user')) return '该邮箱前缀已经被现有用户的默认邮箱保留。';
  if (result.reasons.includes('existing_email_route')) return '该邮箱地址已经存在转发配置。';
  if (result.reasons.includes('reserved_system_prefix')) return '该邮箱前缀属于系统保留地址，无法使用。';
  return '该邮箱前缀当前不可用。';
}

function formatEligibilityReason(reason: string, permission: UserPermission): string {
  switch (reason) {
    case 'trust_level_below_minimum':
      return `当前信任等级不足，需要至少达到 TL ${permission.min_trust_level}`;
    case 'policy_disabled':
      return '当前管理员已关闭该权限策略';
    case 'already_has_permission':
      return '你已经拥有该权限';
    case 'already_has_pending_application':
      return '你已经提交过申请，请等待审核';
    default:
      return reason;
  }
}

function readableErrorMessage(error: unknown, fallback: string): string {
  if (error instanceof APIError) return error.message;
  if (error instanceof Error && error.message.trim() !== '') return error.message;
  return fallback;
}

function formatDate(value?: string): string {
  if (!value) return '尚未保存';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  }).format(date);
}

function normalizePrefix(value: string): string {
  return value.trim().toLowerCase().replace(/[^a-z0-9-]+/g, '-').replace(/^-+|-+$/g, '');
}

function normalizeEmail(value: string): string {
  return value.trim().toLowerCase();
}

function isVerifiedTargetOwned(email: string, targets: UserEmailTarget[]): boolean {
  const normalizedEmail = normalizeEmail(email);
  return targets.some((item) => item.verified && normalizeEmail(item.email) === normalizedEmail);
}

function routeTargetNeedsVerification(targetEmail: string | undefined, verifiedTargets: UserEmailTarget[]): boolean {
  const normalizedTarget = normalizeEmail(targetEmail ?? '');
  if (!normalizedTarget) {
    return false;
  }
  return !verifiedTargets.some((item) => normalizeEmail(item.email) === normalizedTarget);
}
