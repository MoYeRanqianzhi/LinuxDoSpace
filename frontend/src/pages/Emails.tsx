import { useEffect, useMemo, useRef, useState, type FormEvent } from 'react';
import { AnimatePresence, motion } from 'motion/react';
import {
  AlertCircle,
  ArrowRight,
  CheckCircle2,
  Copy,
  KeyRound,
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
import { GlassTooltip } from '../components/GlassTooltip';
import { ToggleSwitch } from '../components/ToggleSwitch';
import {
  APIError,
  checkPublicEmailRouteAvailability,
  createMyAPIToken,
  createMyEmailTarget,
  listMyEmailRoutes,
  listMyAPITokens,
  listMyEmailTargets,
  listMyPermissions,
  revokeMyAPIToken,
  resendMyEmailTargetVerification,
  submitPermissionApplication,
  upsertCatchAllEmailRoute,
  upsertDefaultEmailRoute,
} from '../lib/api';
import type {
  EmailRouteAvailabilityResult,
  EmailRouteTargetType,
  ManagedDomain,
  PermissionStatus,
  UserAPIToken,
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
  const [apiTokens, setApiTokens] = useState<UserAPIToken[]>([]);
  const [loading, setLoading] = useState(false);
  const [permissionError, setPermissionError] = useState('');
  const [routeError, setRouteError] = useState('');
  const [targetError, setTargetError] = useState('');
  const [tokenError, setTokenError] = useState('');

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
  const [resendingTargetIDs, setResendingTargetIDs] = useState<Record<number, boolean>>({});
  const [targetNotice, setTargetNotice] = useState<SectionNotice | null>(null);
  const [targetRowNotices, setTargetRowNotices] = useState<Record<number, SectionNotice>>({});
  const [tokenNotice, setTokenNotice] = useState<SectionNotice | null>(null);
  const [newTokenName, setNewTokenName] = useState('');
  const [creatingToken, setCreatingToken] = useState(false);
  const [createdTokenSecret, setCreatedTokenSecret] = useState('');
  const [revokingTokenPublicIDs, setRevokingTokenPublicIDs] = useState<Record<string, boolean>>({});

  const [applyingPermission, setApplyingPermission] = useState(false);
  const [pledgeModalOpen, setPledgeModalOpen] = useState(false);
  const loadRequestTokenRef = useRef(0);
  const searchRequestTokenRef = useRef(0);
  const resendingTargetIDsRef = useRef<Set<number>>(new Set());

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
  const activeAPITokens = useMemo(
    () => apiTokens.filter((item) => item.email_enabled && !item.revoked_at),
    [apiTokens],
  );
  const selectableTargetOptions = useMemo<GlassSelectOption[]>(() => {
    const options = [
      ...verifiedTargets.map((item) => ({
        value: buildEmailTargetOptionValue(item.email),
        label: item.email,
      })),
      ...activeAPITokens.map((item) => ({
        value: buildAPITokenTargetOptionValue(item.public_id),
        label: `TOKEN · ${item.name} (${item.public_id})`,
      })),
    ];
    return [{ value: '', label: '不转发 / 清空目标' }, ...options];
  }, [activeAPITokens, verifiedTargets]);

  const defaultAddress = defaultRoute?.address ?? (normalizedUsername ? `${normalizedUsername}@${configuredRootDomain}` : '');
  const catchAllAddress = useMemo(() => {
    if (catchAllRoute?.address) return catchAllRoute.address;
    if (permission?.target?.trim()) return permission.target.trim();
    return normalizedUsername ? `*@${normalizedUsername}.${configuredRootDomain}` : `*@<用户名>.${configuredRootDomain}`;
  }, [catchAllRoute?.address, configuredRootDomain, normalizedUsername, permission?.target]);
  const searchRootDomain = defaultRoute?.root_domain ?? searchResult?.root_domain ?? configuredRootDomain;
  const pledgeText = permission?.pledge_text?.trim() ?? '';
  const pendingTargetCount = emailTargets.length - verifiedTargets.length;
  const tokenTargetCount = activeAPITokens.length;
  const defaultTargetNeedsVerification = useMemo(
    () => routeTargetNeedsVerification(defaultRoute, verifiedTargets),
    [defaultRoute, verifiedTargets],
  );
  const catchAllTargetNeedsVerification = useMemo(
    () => routeTargetNeedsVerification(catchAllRoute, verifiedTargets),
    [catchAllRoute, verifiedTargets],
  );

  useEffect(() => {
    if (!authenticated) {
      loadRequestTokenRef.current += 1;
      searchRequestTokenRef.current += 1;
      setPermission(null);
      setRoutes([]);
      setEmailTargets([]);
      setApiTokens([]);
      setLoading(false);
      setPermissionError('');
      setRouteError('');
      setTargetError('');
      setTokenError('');
      setDefaultTarget('');
      setDefaultEnabled(false);
      setCatchAllTarget('');
      setCatchAllEnabled(false);
      setNewTargetEmail('');
      setDefaultNotice(null);
      setCatchAllNotice(null);
      setTargetNotice(null);
      setTargetRowNotices({});
      setTokenNotice(null);
      setNewTokenName('');
      setCreatedTokenSecret('');
      setRevokingTokenPublicIDs({});
      setResendingTargetIDs({});
      resendingTargetIDsRef.current.clear();
      setPledgeModalOpen(false);
      return;
    }

    void loadAuthenticatedData();
  }, [authenticated, user?.id]);

  useEffect(() => {
    setDefaultTarget(defaultRoute ? routeTargetSelectionValue(defaultRoute) : '');
    setDefaultEnabled(defaultRoute?.enabled ?? false);
  }, [defaultRoute]);

  useEffect(() => {
    setCatchAllTarget(catchAllRoute ? routeTargetSelectionValue(catchAllRoute) : '');
    setCatchAllEnabled(catchAllRoute?.enabled ?? false);
  }, [catchAllRoute]);

  async function loadAuthenticatedData(): Promise<void> {
    const requestToken = ++loadRequestTokenRef.current;
    setLoading(true);
    setPermissionError('');
    setRouteError('');
    setTargetError('');

    const [permissionResult, routeResult, targetResult, tokenResult] = await Promise.allSettled([
      listMyPermissions(),
      listMyEmailRoutes(),
      listMyEmailTargets(),
      listMyAPITokens(),
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

    if (tokenResult.status === 'fulfilled') {
      setApiTokens(tokenResult.value);
      setTokenError('');
    } else {
      const maybeTokenError = tokenResult.reason;
      if (maybeTokenError instanceof APIError && maybeTokenError.code === 'not_found') {
        setApiTokens([]);
        setTokenError('');
      } else {
        setApiTokens([]);
        setTokenError(readableErrorMessage(tokenResult.reason, '无法加载我的 API TOKEN 列表。'));
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
          : `目标邮箱 ${createdTarget.email} 已绑定到你的账号。平台验证邮件已发出，请前往该邮箱确认后点击“刷新状态”。`,
      });
    } catch (error) {
      setTargetNotice({ tone: 'error', message: readableErrorMessage(error, '添加目标邮箱失败。') });
    } finally {
      setCreatingTarget(false);
    }
  }

  async function handleRefreshTargets(): Promise<void> {
    await loadAuthenticatedData();
    setTargetRowNotices({});
    setTargetNotice({
      tone: 'info',
      message: '已刷新目标邮箱状态。若你刚完成邮箱确认，现在应该能看到最新验证结果。',
    });
  }

  async function handleCreateToken(event: FormEvent<HTMLFormElement>): Promise<void> {
    event.preventDefault();
    if (!csrfToken) {
      setTokenNotice({ tone: 'error', message: '当前会话缺少 CSRF Token，请重新登录后再试。' });
      return;
    }

    const tokenName = newTokenName.trim();
    if (!tokenName) {
      setTokenNotice({ tone: 'error', message: '请输入 TOKEN 名称。' });
      return;
    }

    try {
      setCreatingToken(true);
      setTokenNotice(null);
      const result = await createMyAPIToken({ name: tokenName, email_enabled: true }, csrfToken);
      setApiTokens((currentItems) => upsertAPIToken(currentItems, result.token));
      setCreatedTokenSecret(result.raw_token);
      setNewTokenName('');
      setTokenNotice({
        tone: 'success',
        message: `TOKEN ${result.token.name} 已创建。请立即复制保存原始密钥，离开当前提示后将无法再次查看。`,
      });
    } catch (error) {
      setTokenNotice({ tone: 'error', message: readableErrorMessage(error, '创建 API TOKEN 失败。') });
    } finally {
      setCreatingToken(false);
    }
  }

  async function handleRevokeToken(publicID: string): Promise<void> {
    if (!csrfToken) {
      setTokenNotice({ tone: 'error', message: '当前会话缺少 CSRF Token，请重新登录后再试。' });
      return;
    }

    try {
      setRevokingTokenPublicIDs((current) => ({ ...current, [publicID]: true }));
      const item = await revokeMyAPIToken(publicID, csrfToken);
      setApiTokens((currentItems) => upsertAPIToken(currentItems, item));
      setTokenNotice({ tone: 'info', message: `TOKEN ${item.name} 已撤销，新的实时连接将不再被接受。` });
    } catch (error) {
      setTokenNotice({ tone: 'error', message: readableErrorMessage(error, '撤销 API TOKEN 失败。') });
    } finally {
      setRevokingTokenPublicIDs((current) => {
        const next = { ...current };
        delete next[publicID];
        return next;
      });
    }
  }

  async function handleCopyCreatedToken(): Promise<void> {
    if (!createdTokenSecret) {
      return;
    }
    try {
      await navigator.clipboard.writeText(createdTokenSecret);
      setTokenNotice({ tone: 'success', message: 'TOKEN 已复制到剪贴板。' });
    } catch {
      setTokenNotice({ tone: 'info', message: '浏览器未允许自动复制，请手动复制下方原始 TOKEN。' });
    }
  }

  async function handleResendTargetVerification(targetID: number): Promise<void> {
    if (!csrfToken) {
      setTargetRowNotices((current) => ({
        ...current,
        [targetID]: { tone: 'error', message: '当前会话缺少 CSRF Token，请重新登录后再试。' },
      }));
      return;
    }
    if (resendingTargetIDsRef.current.has(targetID)) {
      return;
    }

    try {
      resendingTargetIDsRef.current.add(targetID);
      setResendingTargetIDs((current) => ({ ...current, [targetID]: true }));
      setTargetRowNotices((current) => {
        const next = { ...current };
        delete next[targetID];
        return next;
      });
      const updatedTarget = await resendMyEmailTargetVerification(targetID, csrfToken);
      setEmailTargets((currentTargets) => upsertEmailTarget(currentTargets, updatedTarget));
      setTargetRowNotices((current) => ({
        ...current,
        [targetID]: { tone: 'success', message: `已重新向 ${updatedTarget.email} 发送平台验证邮件，请前往目标邮箱查收。` },
      }));
    } catch (error) {
      setTargetRowNotices((current) => ({
        ...current,
        [targetID]: { tone: 'error', message: readableErrorMessage(error, '重新发送验证邮件失败。') },
      }));
    } finally {
      resendingTargetIDsRef.current.delete(targetID);
      setResendingTargetIDs((current) => {
        const next = { ...current };
        delete next[targetID];
        return next;
      });
    }
  }

  async function handleSaveDefault(event: FormEvent<HTMLFormElement>): Promise<void> {
    event.preventDefault();
    if (!csrfToken) {
      setDefaultNotice({ tone: 'error', message: '当前会话缺少 CSRF Token，请重新登录后再试。' });
      return;
    }

    const nextTarget = parseRouteTargetSelection(defaultTarget);
    if (defaultEnabled && !nextTarget.configured) {
      setDefaultNotice({ tone: 'error', message: '启用默认邮箱转发前，请先选择一个已验证的目标邮箱或可用 TOKEN。' });
      return;
    }
    if (nextTarget.targetType === 'email' && nextTarget.targetEmail && !isVerifiedTargetOwned(nextTarget.targetEmail, verifiedTargets)) {
      setDefaultNotice({ tone: 'error', message: '当前只能选择已经绑定到你账号且已完成平台验证的目标邮箱。' });
      return;
    }
    if (nextTarget.targetType === 'api_token' && nextTarget.targetTokenPublicID && !isActiveEmailTokenOwned(nextTarget.targetTokenPublicID, activeAPITokens)) {
      setDefaultNotice({ tone: 'error', message: '当前只能选择属于你本人、未撤销且支持 EMAIL 的 TOKEN。' });
      return;
    }

    try {
      setSavingDefault(true);
      setDefaultNotice(null);
      const savedRoute = await upsertDefaultEmailRoute(
        {
          target_type: nextTarget.targetType,
          target_email: nextTarget.targetEmail,
          target_token_public_id: nextTarget.targetTokenPublicID,
          enabled: nextTarget.configured ? defaultEnabled : false,
        },
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

    const nextTarget = parseRouteTargetSelection(catchAllTarget);
    if (catchAllEnabled && !nextTarget.configured) {
      setCatchAllNotice({ tone: 'error', message: '启用邮箱泛解析转发前，请先选择一个已验证的目标邮箱或可用 TOKEN。' });
      return;
    }
    if (nextTarget.targetType === 'email' && nextTarget.targetEmail && !isVerifiedTargetOwned(nextTarget.targetEmail, verifiedTargets)) {
      setCatchAllNotice({ tone: 'error', message: '当前只能选择已经绑定到你账号且已完成平台验证的目标邮箱。' });
      return;
    }
    if (nextTarget.targetType === 'api_token' && nextTarget.targetTokenPublicID && !isActiveEmailTokenOwned(nextTarget.targetTokenPublicID, activeAPITokens)) {
      setCatchAllNotice({ tone: 'error', message: '当前只能选择属于你本人、未撤销且支持 EMAIL 的 TOKEN。' });
      return;
    }

    try {
      setSavingCatchAll(true);
      setCatchAllNotice(null);
      const savedRoute = await upsertCatchAllEmailRoute(
        {
          target_type: nextTarget.targetType,
          target_email: nextTarget.targetEmail,
          target_token_public_id: nextTarget.targetTokenPublicID,
          enabled: nextTarget.configured ? catchAllEnabled : false,
        },
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
            搜索功能对所有访客开放。登录后，你可以先绑定自己的转发目标邮箱或创建 TOKEN，再管理默认邮箱
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
                  <p className="mt-1 text-sm text-gray-600 dark:text-gray-300">邮箱能力分成公开搜索、目标邮箱绑定、TOKEN 转发、默认邮箱和权限邮箱五部分。</p>
              </div>
            </div>

            <InfoBlock title="默认邮箱" description={normalizedUsername ? `每位用户默认保留 ${normalizedUsername}@${configuredRootDomain}，但必须先绑定自己的目标邮箱后才能转发。` : '每位用户都会默认保留一个与用户名同名的邮箱地址。'} />
            <InfoBlock title="我的转发目标" description="先在“我的转发目标”里绑定目标邮箱。新增后平台会向该邮箱发送确认邮件，验证完成后该目标才会出现在下拉选择器里。" />
            <InfoBlock title="TOKEN 转发" description="你可以创建支持 EMAIL 的 API TOKEN，把它作为实时收件目标。TOKEN 在线时会实时收到邮件事件；若没有连接，邮件会直接丢弃而不会堆积在服务器里。" />
            <InfoBlock title="我的邮箱列表" description="这里会展示当前账号已经存在或默认保留的邮箱行，包括默认邮箱、已存在的自定义邮箱以及已配置的邮箱泛解析。" />
            <InfoBlock title="邮箱泛解析权限" description="邮箱泛解析不是默认开放功能。只有满足权限条件的用户才可以申请，并在通过后配置转发目标。" />
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
                  ? '你现在仍可使用上方搜索功能。待登录状态加载完成后，再进入目标邮箱绑定、默认邮箱和邮箱泛解析配置。'
                  : '搜索功能无需登录，但目标邮箱绑定、TOKEN 管理、默认邮箱配置、我的邮箱列表和邮箱泛解析权限申请都需要使用 Linux Do 账号登录。'}
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
                          <td className="px-5 py-4 align-top text-sm text-gray-700 dark:text-gray-200">
                            {route.target_display || '尚未设置转发目标'}
                            {route.target_type === 'api_token' ? (
                              <div className="mt-1 text-xs text-violet-600 dark:text-violet-300">实时 TOKEN 目标</div>
                            ) : null}
                          </td>
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
                  <p className="mt-1 text-sm text-gray-600 dark:text-gray-300">先绑定并验证目标邮箱，再把它用于默认邮箱或邮箱泛解析转发。</p>
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
              每个目标邮箱都会和当前 LinuxDoSpace 账号绑定，其他用户不能重复占用。首次添加时，平台会向该邮箱发送确认邮件；只有完成确认后，这个目标邮箱才会出现在下方配置下拉框中。
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
                你当前还没有绑定任何目标邮箱。先添加一个你自己的邮箱地址，完成平台验证后，它才会出现在默认邮箱和邮箱泛解析的下拉框里。
              </div>
            ) : (
              <div className="overflow-x-auto rounded-3xl border border-white/15 bg-white/35 dark:border-white/10 dark:bg-black/20">
                <table className="w-full min-w-[760px] border-collapse text-left">
                  <thead>
                    <tr className="border-b border-white/15 text-sm text-gray-600 dark:border-white/10 dark:text-gray-300">
                      <th className="px-5 py-4 font-semibold">目标邮箱</th>
                      <th className="px-5 py-4 font-semibold">验证状态</th>
                      <th className="px-5 py-4 font-semibold">平台状态</th>
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
                              {item.verified_at ? `验证通过：${formatDate(item.verified_at)}` : '等待你在目标邮箱中确认平台验证邮件。'}
                            </div>
                          </td>
                          <td className="px-5 py-4 align-top text-sm text-gray-700 dark:text-gray-200">
                            {item.verified ? '目标邮箱已可用于转发' : '等待目标邮箱完成平台验证'}
                          </td>
                          <td className="px-5 py-4 align-top text-sm text-gray-600 dark:text-gray-300">
                            <div>{item.last_verification_sent_at ? `验证邮件发送于 ${formatDate(item.last_verification_sent_at)}` : `最近更新于 ${formatDate(item.updated_at)}`}</div>
                            {!item.verified ? (
                              <>
                                <button
                                  type="button"
                                  onClick={() => void handleResendTargetVerification(item.id)}
                                  disabled={Boolean(resendingTargetIDs[item.id])}
                                  className="mt-3 inline-flex items-center gap-2 rounded-xl border border-sky-200 bg-white/70 px-3 py-2 text-xs font-semibold text-sky-700 transition hover:bg-sky-50 disabled:cursor-not-allowed disabled:opacity-60 dark:border-sky-800/40 dark:bg-black/20 dark:text-sky-300 dark:hover:bg-sky-900/20"
                                >
                                  {resendingTargetIDs[item.id] ? <LoaderCircle className="animate-spin" size={14} /> : <RefreshCw size={14} />}
                                  重新发送验证
                                </button>
                                {targetRowNotices[item.id] ? (
                                  <div className={`mt-3 rounded-2xl px-3 py-2 text-xs leading-6 ${
                                    targetRowNotices[item.id].tone === 'success'
                                      ? 'border border-emerald-200/70 bg-emerald-50/80 text-emerald-700 dark:border-emerald-700/40 dark:bg-emerald-950/25 dark:text-emerald-200'
                                      : 'border border-red-300/60 bg-red-50/80 text-red-700 dark:border-red-500/30 dark:bg-red-950/25 dark:text-red-200'
                                  }`}>
                                    {targetRowNotices[item.id].message}
                                  </div>
                                ) : null}
                              </>
                            ) : null}
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            )}
          </GlassCard>

          <GlassCard className="space-y-5">
            <div className="flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
              <div className="flex items-center gap-3">
                <div className="rounded-2xl bg-violet-500/15 p-3 text-violet-700 dark:text-violet-300"><KeyRound size={20} /></div>
                <div>
                  <h2 className="text-xl font-bold text-gray-900 dark:text-white">我的 API TOKEN</h2>
                  <p className="mt-1 text-sm text-gray-600 dark:text-gray-300">创建后可被选为实时收件目标，供 SDK 或自建客户端通过 HTTPS 实时流接收邮件事件。</p>
                </div>
              </div>
            </div>

            {tokenError ? <InlineNotice tone="error" message={`TOKEN 列表加载失败：${tokenError}`} /> : null}
            {tokenNotice ? <InlineNotice tone={tokenNotice.tone} message={tokenNotice.message} /> : null}

            {createdTokenSecret ? (
              <div className="rounded-3xl border border-violet-300/35 bg-violet-50/80 p-5 dark:border-violet-700/35 dark:bg-violet-950/20">
                <div className="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
                  <div>
                    <div className="text-sm font-semibold text-violet-900 dark:text-violet-100">新 TOKEN 原始密钥</div>
                    <div className="mt-2 text-sm leading-7 text-violet-900/80 dark:text-violet-100/90">
                      这串原始 TOKEN 只会展示这一次。请立即复制保存，之后页面只会保留公开 ID 和名称，不会再次返回原始密钥。
                    </div>
                  </div>
                  <button
                    type="button"
                    onClick={() => void handleCopyCreatedToken()}
                    className="inline-flex items-center gap-2 rounded-2xl bg-violet-600 px-4 py-3 text-sm font-semibold text-white transition hover:bg-violet-700"
                  >
                    <Copy size={16} />
                    复制 TOKEN
                  </button>
                </div>
                <div className="mt-4 rounded-2xl border border-violet-200/70 bg-white/75 px-4 py-3 font-mono text-sm break-all text-violet-900 dark:border-violet-700/35 dark:bg-black/25 dark:text-violet-100">
                  {createdTokenSecret}
                </div>
              </div>
            ) : null}

            <div className="grid gap-3 md:grid-cols-3">
              <InfoStat title="可用 TOKEN" value={`${tokenTargetCount} 个`} />
              <InfoStat title="全部 TOKEN" value={`${apiTokens.length} 个`} />
              <InfoStat title="能力" value="EMAIL 实时流" />
            </div>

            <div className="rounded-2xl border border-white/15 bg-white/35 p-4 text-sm leading-7 text-gray-700 dark:border-white/10 dark:bg-black/20 dark:text-gray-200">
              TOKEN 被设置为邮箱目标后，只有在客户端保持连接时才会收到实时邮件事件；如果没有连接，服务器会直接丢弃该目标邮件，不会为了 TOKEN 目标额外堆积队列。
            </div>

            <form className="space-y-4" onSubmit={(event) => void handleCreateToken(event)}>
              <div className="grid gap-3 lg:grid-cols-[1fr_auto]">
                <div className="flex min-w-0 items-center rounded-2xl border border-white/20 bg-white/55 px-4 py-3 shadow-inner dark:border-white/10 dark:bg-black/35">
                  <input
                    type="text"
                    value={newTokenName}
                    onChange={(event) => setNewTokenName(event.target.value)}
                    placeholder="例如 Python SDK / 邮件机器人 / 自建客户端"
                    className="min-w-0 flex-1 bg-transparent text-base text-gray-900 outline-none placeholder:text-gray-400 dark:text-white dark:placeholder:text-gray-500"
                  />
                </div>
                <button
                  type="submit"
                  disabled={creatingToken}
                  className="inline-flex items-center justify-center gap-2 rounded-2xl bg-gradient-to-r from-violet-500 to-fuchsia-600 px-5 py-3 font-semibold text-white shadow-lg transition hover:from-violet-600 hover:to-fuchsia-700 disabled:cursor-not-allowed disabled:opacity-60"
                >
                  {creatingToken ? <LoaderCircle className="animate-spin" size={18} /> : <Plus size={18} />}
                  创建 TOKEN
                </button>
              </div>
            </form>

            {apiTokens.length === 0 ? (
              <div className="rounded-3xl border border-dashed border-white/20 bg-white/25 p-6 text-sm leading-7 text-gray-700 dark:border-white/10 dark:bg-black/15 dark:text-gray-200">
                你当前还没有创建任何 API TOKEN。创建后，它们会出现在默认邮箱和邮箱泛解析的目标下拉框里，可直接作为实时收件目标使用。
              </div>
            ) : (
              <div className="overflow-x-auto rounded-3xl border border-white/15 bg-white/35 dark:border-white/10 dark:bg-black/20">
                <table className="w-full min-w-[820px] border-collapse text-left">
                  <thead>
                    <tr className="border-b border-white/15 text-sm text-gray-600 dark:border-white/10 dark:text-gray-300">
                      <th className="px-5 py-4 font-semibold">名称</th>
                      <th className="px-5 py-4 font-semibold">公开 ID</th>
                      <th className="px-5 py-4 font-semibold">能力</th>
                      <th className="px-5 py-4 font-semibold">最近使用</th>
                      <th className="px-5 py-4 font-semibold">状态</th>
                    </tr>
                  </thead>
                  <tbody>
                    {apiTokens.map((item) => {
                      const isRevoked = Boolean(item.revoked_at);
                      return (
                        <tr key={item.public_id} className="border-b border-white/10 last:border-b-0 hover:bg-white/30 dark:border-white/5 dark:hover:bg-white/5">
                          <td className="px-5 py-4 align-top">
                            <div className="font-semibold text-gray-900 dark:text-white">{item.name}</div>
                            <div className="mt-1 text-sm text-gray-500 dark:text-gray-400">创建于 {formatDate(item.created_at)}</div>
                          </td>
                          <td className="px-5 py-4 align-top text-sm font-mono text-gray-700 dark:text-gray-200">{item.public_id}</td>
                          <td className="px-5 py-4 align-top text-sm text-gray-700 dark:text-gray-200">{item.email_enabled ? 'EMAIL 实时流' : '未启用'}</td>
                          <td className="px-5 py-4 align-top text-sm text-gray-700 dark:text-gray-200">{item.last_used_at ? formatDate(item.last_used_at) : '尚未使用'}</td>
                          <td className="px-5 py-4 align-top">
                            <div className="flex flex-col items-start gap-3">
                              <StatusChip {...(isRevoked
                                ? { label: '已撤销', className: 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300' }
                                : { label: '可用', className: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/25 dark:text-emerald-300' })} />
                              {!isRevoked ? (
                                <button
                                  type="button"
                                  onClick={() => void handleRevokeToken(item.public_id)}
                                  disabled={Boolean(revokingTokenPublicIDs[item.public_id])}
                                  className="inline-flex items-center gap-2 rounded-xl border border-red-200 bg-white/70 px-3 py-2 text-xs font-semibold text-red-700 transition hover:bg-red-50 disabled:cursor-not-allowed disabled:opacity-60 dark:border-red-800/35 dark:bg-black/20 dark:text-red-300 dark:hover:bg-red-950/20"
                                >
                                  {revokingTokenPublicIDs[item.public_id] ? <LoaderCircle className="animate-spin" size={14} /> : <RefreshCw size={14} />}
                                  撤销 TOKEN
                                </button>
                              ) : null}
                            </div>
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
              {defaultRoute?.target_type === 'api_token' && defaultRoute.target_token_public_id && !isActiveEmailTokenOwned(defaultRoute.target_token_public_id, activeAPITokens) ? (
                <InlineNotice tone="info" message={`当前已保存的 TOKEN 目标 ${defaultRoute.target_token_public_id} 不再可用。你可以重新创建 TOKEN，或改选其他目标。`} />
              ) : null}

              <form className="space-y-4" onSubmit={(event) => void handleSaveDefault(event)}>
                <div className="space-y-2">
                  <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">转发目标</label>
                  <GlassSelect
                    options={selectableTargetOptions}
                    value={defaultTarget}
                    onChange={setDefaultTarget}
                    placeholder={selectableTargetOptions.length > 1 ? '请选择目标邮箱或 TOKEN' : '暂无可用目标'}
                    disabled={savingDefault}
                  />
                  <div className="text-sm leading-7 text-gray-600 dark:text-gray-300">你可以选择已验证的目标邮箱，也可以选择支持 EMAIL 的 TOKEN 作为实时收件目标。</div>
                </div>

                <ToggleSwitch
                  title="启用默认邮箱转发"
                  description="关闭后会保留邮箱地址，但暂时不再转发邮件。"
                  checked={defaultEnabled}
                  onCheckedChange={setDefaultEnabled}
                  disabled={savingDefault}
                />

                <div className="rounded-2xl border border-white/15 bg-white/35 p-4 text-sm leading-7 text-gray-700 dark:border-white/10 dark:bg-black/20 dark:text-gray-200">
                  每个用户都会自动保留一个与用户名同名的邮箱地址。你可以选择已验证的目标邮箱进行转发，也可以选择 TOKEN 进行实时收件，或直接清空目标来停用转发。
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
              {catchAllRoute?.target_type === 'api_token' && catchAllRoute.target_token_public_id && !isActiveEmailTokenOwned(catchAllRoute.target_token_public_id, activeAPITokens) ? (
                <InlineNotice tone="info" message={`当前已保存的 TOKEN 目标 ${catchAllRoute.target_token_public_id} 不再可用。你可以重新创建 TOKEN，或改选其他目标。`} />
              ) : null}
              {!permission && !permissionError ? <InlineNotice tone="info" message="当前后端没有返回邮箱泛解析权限配置，暂时无法管理此功能。" /> : null}

              <div className="grid gap-3 md:grid-cols-2">
                <InfoStat title="目标地址" value={catchAllAddress} mono />
                <InfoStat title="当前状态" value={describePermissionStatusLabel(permission?.status)} />
              </div>

              {permission ? (
                <>
                  <div className="grid gap-3 md:grid-cols-3">
                    <InfoStat title="审核状态" value={describePermissionStatusLabel(permission.status)} />
                    <InfoStat title="最低等级" value={`TL ${permission.min_trust_level}`} />
                    <InfoStat title="当前模式" value={describeCatchAllAccessMode(permission)} />
                  </div>

                  {permission.catch_all_access ? (
                    <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
                      <InfoStat
                        title="订阅到期"
                        value={permission.catch_all_access.subscription_expires_at ? formatDate(permission.catch_all_access.subscription_expires_at) : '未订阅'}
                      />
                      <InfoStat
                        title="剩余次数"
                        value={permission.catch_all_access.remaining_count.toLocaleString('zh-CN')}
                        tooltip={buildCatchAllBalanceTooltip(permission.catch_all_access)}
                      />
                      <InfoStat
                        title="今日剩余"
                        value={permission.catch_all_access.daily_remaining_count.toLocaleString('zh-CN')}
                      />
                      <InfoStat
                        title="单日上限"
                        value={permission.catch_all_access.effective_daily_limit.toLocaleString('zh-CN')}
                      />
                    </div>
                  ) : null}

                  {permission.eligibility_reasons.length > 0 ? (
                    <div className="rounded-2xl border border-amber-300/35 bg-amber-50/80 p-4 text-sm leading-7 text-amber-900 dark:border-amber-500/20 dark:bg-amber-950/25 dark:text-amber-100">
                      <div className="mb-2 font-semibold">当前暂不可直接申请：</div>
                      <div className="space-y-1">
                        {permission.eligibility_reasons.map((reason) => (
                          <div key={reason}>- {formatEligibilityReason(reason, permission)}</div>
                        ))}
                      </div>
                    </div>
                  ) : null}

                  {permission.application ? (
                    <div className="rounded-2xl border border-white/15 bg-white/35 p-4 text-sm leading-7 text-gray-700 dark:border-white/10 dark:bg-black/20 dark:text-gray-200">
                      <div>最近申请：{formatDate(permission.application.created_at)}</div>
                      <div>最近变更：{formatDate(permission.application.updated_at)}</div>
                      <div>审核备注：{permission.application.review_note || '暂无审核备注'}</div>
                    </div>
                  ) : null}

                  {permission.can_manage_route ? (
                    <form className="space-y-4" onSubmit={(event) => void handleSaveCatchAll(event)}>
                      <div className="space-y-2">
                        <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">转发目标</label>
                        <GlassSelect
                          options={selectableTargetOptions}
                          value={catchAllTarget}
                          onChange={setCatchAllTarget}
                          placeholder={selectableTargetOptions.length > 1 ? '请选择目标邮箱或 TOKEN' : '暂无可用目标'}
                          disabled={savingCatchAll}
                        />
                        <div className="text-sm leading-7 text-gray-600 dark:text-gray-300">保存后会把整段命名空间 {catchAllAddress} 转发到所选目标。目标可以是已验证邮箱，也可以是支持 EMAIL 的 TOKEN。</div>
                      </div>

                      <ToggleSwitch
                        title="启用邮箱泛解析转发"
                        description="关闭后会保留命名空间地址，但暂停整个命名空间的邮件转发。"
                        checked={catchAllEnabled}
                        onCheckedChange={setCatchAllEnabled}
                        disabled={savingCatchAll}
                      />

                      <div className="rounded-2xl border border-white/15 bg-white/35 p-4 text-sm leading-7 text-gray-700 dark:border-white/10 dark:bg-black/20 dark:text-gray-200">
                        当前默认邮箱与邮箱泛解析都会由服务端中转处理，额度与订阅状态均以后端实时判断结果为准。
                      </div>

                      <button
                        type="submit"
                        disabled={savingCatchAll}
                        className="inline-flex items-center gap-2 rounded-2xl bg-gradient-to-r from-violet-500 to-fuchsia-600 px-5 py-3 font-semibold text-white shadow-lg transition hover:from-violet-600 hover:to-fuchsia-700 disabled:cursor-not-allowed disabled:opacity-60"
                      >
                        {savingCatchAll ? <LoaderCircle className="animate-spin" size={18} /> : <Sparkles size={18} />}
                        保存邮箱泛解析
                      </button>
                    </form>
                  ) : (
                    <div className="rounded-2xl border border-white/15 bg-white/35 p-4 text-sm leading-7 text-gray-700 dark:border-white/10 dark:bg-black/20 dark:text-gray-200">
                      <div className="mb-4">当前还没有获得邮箱泛解析配置权限。你可以先查看承诺书，再通过按钮提交申请。</div>
                      <div className="flex flex-wrap gap-3">
                        <button
                          type="button"
                          onClick={() => setPledgeModalOpen(true)}
                          className="rounded-2xl border border-white/20 bg-white/70 px-4 py-3 font-medium text-gray-900 transition hover:bg-white dark:border-white/10 dark:bg-black/35 dark:text-white dark:hover:bg-black/50"
                        >
                          查看承诺书
                        </button>
                        <button
                          type="button"
                          disabled={!permission.can_apply || applyingPermission}
                          onClick={() => setPledgeModalOpen(true)}
                          className="inline-flex items-center gap-2 rounded-2xl bg-gradient-to-r from-sky-500 to-cyan-500 px-5 py-3 font-semibold text-white shadow-lg transition hover:from-sky-600 hover:to-cyan-600 disabled:cursor-not-allowed disabled:opacity-60"
                        >
                          {applyingPermission ? <LoaderCircle className="animate-spin" size={18} /> : <Sparkles size={18} />}
                          {permission.can_apply ? '查看承诺书并申请' : '当前不可申请'}
                        </button>
                      </div>
                    </div>
                  )}
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
  mono?: boolean;
  tooltip?: string;
}

function InfoStat({ title, value, mono = false, tooltip }: InfoStatProps) {
  return (
    <GlassTooltip content={tooltip}>
      <div
        tabIndex={tooltip ? 0 : undefined}
        className={`rounded-2xl border border-white/15 bg-white/35 p-4 outline-none dark:border-white/10 dark:bg-black/20 ${tooltip ? 'cursor-help' : ''}`}
      >
        <div className="text-xs font-semibold uppercase tracking-[0.18em] text-gray-500 dark:text-gray-400">{title}</div>
        <div className={`mt-2 text-base font-semibold text-gray-900 dark:text-white ${mono ? 'font-mono break-all' : ''}`}>{value}</div>
      </div>
    </GlassTooltip>
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
    target_type: 'email',
    target_email: '',
    target_display: '',
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

function upsertAPIToken(items: UserAPIToken[], nextItem: UserAPIToken): UserAPIToken[] {
  const existingIndex = items.findIndex((item) => item.public_id === nextItem.public_id);
  if (existingIndex >= 0) {
    return items.map((item, index) => (index === existingIndex ? nextItem : item));
  }
  return [...items, nextItem].sort((left, right) => {
    const leftTime = Date.parse(left.created_at);
    const rightTime = Date.parse(right.created_at);
    if (!Number.isNaN(leftTime) && !Number.isNaN(rightTime) && leftTime !== rightTime) {
      return rightTime - leftTime;
    }
    return right.public_id.localeCompare(left.public_id);
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

function describePermissionStatusLabel(status?: PermissionStatus): string {
  switch (status) {
    case 'approved':
      return '已通过';
    case 'pending':
      return '待审核';
    case 'rejected':
      return '未通过';
    default:
      return '尚未申请';
  }
}

function describeCatchAllAccessMode(permission: UserPermission): string {
  if (!permission.catch_all_access) return '未开通';
  switch (permission.catch_all_access.access_mode) {
    case 'subscription':
      return '订阅时长';
    case 'reward_then_quantity':
      return '临时奖励 + 按量额度';
    case 'temporary_reward':
      return '临时奖励';
    case 'quantity':
      return '按量额度';
    default:
      return permission.status === 'approved' ? '已授权' : '未开通';
  }
}

function buildCatchAllBalanceTooltip(access: NonNullable<UserPermission['catch_all_access']>): string {
  const lines = [
    `永久次数：${access.permanent_remaining_count.toLocaleString('zh-CN')}`,
    `临时奖励：${access.temporary_reward_count.toLocaleString('zh-CN')}`,
  ];
  if (access.temporary_reward_count > 0 && access.temporary_reward_expires_at) {
    lines.push(`临时奖励到期：${formatDate(access.temporary_reward_expires_at)}`);
  }
  lines.push('说明：显示值 = 永久次数 + 仍在有效期内的临时奖励。');
  return lines.join('\n');
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
      return '邮箱泛解析';
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

function isActiveEmailTokenOwned(publicID: string, tokens: UserAPIToken[]): boolean {
  const normalizedID = publicID.trim();
  return tokens.some((item) => item.public_id === normalizedID && item.email_enabled && !item.revoked_at);
}

function routeTargetNeedsVerification(route: UserEmailRoute | null | undefined, verifiedTargets: UserEmailTarget[]): boolean {
  if (!route || route.target_type !== 'email') {
    return false;
  }
  const normalizedTarget = normalizeEmail(route.target_email ?? '');
  if (!normalizedTarget) {
    return false;
  }
  return !verifiedTargets.some((item) => normalizeEmail(item.email) === normalizedTarget);
}

function buildEmailTargetOptionValue(email: string): string {
  return `email:${normalizeEmail(email)}`;
}

function buildAPITokenTargetOptionValue(publicID: string): string {
  return `api_token:${publicID.trim()}`;
}

function routeTargetSelectionValue(route: UserEmailRoute): string {
  if (route.target_type === 'api_token' && route.target_token_public_id) {
    return buildAPITokenTargetOptionValue(route.target_token_public_id);
  }
  if (route.target_email) {
    return buildEmailTargetOptionValue(route.target_email);
  }
  return '';
}

function parseRouteTargetSelection(value: string): {
  targetType: EmailRouteTargetType;
  targetEmail: string;
  targetTokenPublicID: string;
  configured: boolean;
} {
  const normalizedValue = value.trim();
  if (!normalizedValue) {
    return {
      targetType: 'email',
      targetEmail: '',
      targetTokenPublicID: '',
      configured: false,
    };
  }
  if (normalizedValue.startsWith('api_token:')) {
    return {
      targetType: 'api_token',
      targetEmail: '',
      targetTokenPublicID: normalizedValue.slice('api_token:'.length).trim(),
      configured: normalizedValue.slice('api_token:'.length).trim() !== '',
    };
  }
  const email = normalizedValue.startsWith('email:') ? normalizedValue.slice('email:'.length).trim() : normalizeEmail(normalizedValue);
  return {
    targetType: 'email',
    targetEmail: email,
    targetTokenPublicID: '',
    configured: email !== '',
  };
}
