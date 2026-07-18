import { format } from 'date-fns';
import { type QuickstartStepName } from './quickstart/steps';
import {
  type AgentCreatedFilter,
  type CustomCreatedFilter,
  type AgentStatusFilter,
  type AgentTemplate,
  type I18nMsg,
  type ManagedAgentSection,
  type ManagedEntitySection,
  type ResourceConfig,
} from './types';
import { titleCase } from './utils';

export function managedSectionKey(section: ManagedAgentSection | ManagedEntitySection) {
  switch (section) {
    case 'quickstart':
      return 'quickstart';
    case 'credential-vaults':
      return 'credentialVaults';
    case 'memory-stores':
      return 'memoryStores';
    default:
      return section;
  }
}

export function managedMessage(
  msg: I18nMsg,
  section: ManagedAgentSection | ManagedEntitySection,
  suffix: string,
  fallback: string,
) {
  return msg(`managedAgents.${managedSectionKey(section)}.${suffix}`, fallback);
}

export function templateTitle(template: AgentTemplate, msg: I18nMsg) {
  return msg(`managedAgents.quickstart.templates.${template.id}.title`, template.title);
}

export function templateBody(template: AgentTemplate, msg: I18nMsg) {
  return msg(`managedAgents.quickstart.templates.${template.id}.body`, template.body);
}

export function templateSearchText(template: AgentTemplate, msg: I18nMsg) {
  return [
    templateTitle(template, msg),
    templateBody(template, msg),
    ...(template.tags?.map((tag) => tag.label) ?? []),
  ].join(' ');
}

export function quickstartStepLabel(step: QuickstartStepName, msg: I18nMsg) {
  switch (step) {
    case 'Create agent':
      return msg('managedAgents.quickstart.steps.createAgent', step);
    case 'Configure environment':
      return msg('managedAgents.quickstart.steps.configureEnvironment', step);
    case 'Start session':
      return msg('managedAgents.quickstart.steps.startSession', step);
    case 'Integrate':
      return msg('managedAgents.quickstart.steps.integrate', step);
  }
}

export function resourceTitle(config: ResourceConfig, msg: I18nMsg) {
  return managedMessage(msg, config.section, 'title', config.title);
}

export function resourceDescription(config: ResourceConfig, msg: I18nMsg) {
  return managedMessage(msg, config.section, 'description', config.description);
}

export function resourceCreateLabel(config: ResourceConfig, msg: I18nMsg) {
  return config.createLabel ? managedMessage(msg, config.section, 'createLabel', config.createLabel) : undefined;
}

export function resourceSearchPlaceholder(config: ResourceConfig, msg: I18nMsg) {
  return managedMessage(msg, config.section, 'searchPlaceholder', config.searchPlaceholder);
}

export function resourceEmptyTitle(config: ResourceConfig, msg: I18nMsg) {
  return managedMessage(msg, config.section, 'emptyTitle', config.emptyTitle);
}

export function resourceEmptyBody(config: ResourceConfig, msg: I18nMsg) {
  return config.emptyBody ? managedMessage(msg, config.section, 'emptyBody', config.emptyBody) : undefined;
}

export function resourceEmptyAction(config: ResourceConfig, msg: I18nMsg) {
  return config.emptyAction ? managedMessage(msg, config.section, 'emptyAction', config.emptyAction) : undefined;
}

export function entityKindLabel(section: ManagedEntitySection, msg?: I18nMsg) {
  const key = managedSectionKey(section);
  switch (section) {
    case 'sessions':
      return msg ? msg(`managedAgents.${key}.kind`, 'session') : 'session';
    case 'deployments':
      return msg ? msg(`managedAgents.${key}.kind`, 'deployment') : 'deployment';
    case 'environments':
      return msg ? msg(`managedAgents.${key}.kind`, 'environment') : 'environment';
    case 'credential-vaults':
      return msg ? msg(`managedAgents.${key}.kind`, 'vault') : 'vault';
    case 'memory-stores':
      return msg ? msg(`managedAgents.${key}.kind`, 'memory store') : 'memory store';
  }
}

export function entityKindTitle(section: ManagedEntitySection, msg: I18nMsg) {
  return managedMessage(msg, section, 'kindTitle', titleCase(entityKindLabel(section)));
}

function formatCustomCreatedRange(filter: CustomCreatedFilter, msg?: I18nMsg): string {
  const fallback = msg ? msg('managedAgents.filters.customRange', 'Custom range') : 'Custom range';
  const fromMs = Date.parse(filter.from);
  const toMs = Date.parse(filter.to);
  if (Number.isNaN(fromMs) || Number.isNaN(toMs)) {
    return fallback;
  }
  const fromLabel = format(new Date(fromMs), 'MMM d, yyyy');
  const toLabel = format(new Date(toMs), 'MMM d, yyyy');
  return fromLabel === toLabel ? fromLabel : `${fromLabel} – ${toLabel}`;
}

export function createdFilterLabel(filter: AgentCreatedFilter, msg?: I18nMsg) {
  switch (filter.kind) {
    case 'all':
      return msg ? msg('managedAgents.filters.allTime', 'All time') : 'All time';
    case 'last7':
      return msg ? msg('managedAgents.filters.last7Days', 'Last 7 days') : 'Last 7 days';
    case 'last30':
      return msg ? msg('managedAgents.filters.last30Days', 'Last 30 days') : 'Last 30 days';
    case 'custom':
      return formatCustomCreatedRange(filter, msg);
  }
}

export function statusFilterLabel(filter: AgentStatusFilter, msg?: I18nMsg) {
  const fallback = statusFilterOptions.find((option) => option.value === filter)?.label ?? 'Active';
  if (!msg) {
    return fallback;
  }
  switch (filter) {
    case 'active':
      return msg('common.active', fallback);
    case 'all':
      return msg('common.all', fallback);
  }
}

export function statusFilterOptionsFor(msg: I18nMsg): Array<{ value: AgentStatusFilter; label: string }> {
  return statusFilterOptions.map((option) => ({ ...option, label: statusFilterLabel(option.value, msg) }));
}

export function managedFilterLabel(filter: string, msg: I18nMsg) {
  switch (filter) {
    case 'Created  All time':
      return msg('managedAgents.filters.createdAllTime', 'Created  All time');
    case 'Status  Active':
      return msg('managedAgents.filters.statusActive', 'Status  Active');
    case 'Agent  All':
      return msg('managedAgents.filters.agentAll', 'Agent  All');
    case 'Deployment  All':
      return msg('managedAgents.filters.deploymentAll', 'Deployment  All');
    case 'Status  All':
      return msg('managedAgents.filters.statusAll', 'Status  All');
    default:
      return filter;
  }
}

export function submitLabel(section: ManagedEntitySection, editing: boolean, msg?: I18nMsg) {
  if (editing) {
    return msg ? msg('common.save', 'Save') : 'Save';
  }
  if (section === 'credential-vaults') {
    return msg ? msg('common.continue', 'Continue') : 'Continue';
  }
  if (section === 'sessions') {
    return msg ? msg('managedAgents.sessions.createLabel', 'Create session') : 'Create session';
  }
  if (section === 'memory-stores') {
    return msg ? msg('managedAgents.memoryStores.createLabel', 'Create memory store') : 'Create memory store';
  }
  return msg ? msg('common.create', 'Create') : 'Create';
}

export function entityDialogSubtitle(section: ManagedEntitySection, msg: I18nMsg) {
  if (section === 'memory-stores') {
    return '';
  }
  return managedMessage(msg, section, 'dialogSubtitle', dialogSubtitle(section));
}

export function entityActionLabel(action: 'edit' | 'archive' | 'delete', section: ManagedEntitySection, msg: I18nMsg) {
  const label = entityKindLabel(section, msg);
  switch (action) {
    case 'edit':
      return msg('managedAgents.common.editEntity', 'Edit {label}', { label });
    case 'archive':
      return msg('managedAgents.common.archiveEntity', 'Archive {label}', { label });
    case 'delete':
      return msg('managedAgents.common.deleteEntity', 'Delete {label}', { label });
  }
}

export function managedToastMessage(
  section: ManagedEntitySection,
  action: 'created' | 'updated' | 'archived' | 'deleted',
  msg: I18nMsg,
) {
  return msg(`managedAgents.common.toast.${action}`, '{label} {action}', {
    label: entityKindTitle(section, msg),
    action,
  });
}

export function managedColumnLabel(column: string, msg: I18nMsg) {
  switch (column) {
    case 'ID':
      return msg('common.id', 'ID');
    case 'Name':
      return msg('common.name', 'Name');
    case 'Model':
      return msg('analytics.table.model', 'Model');
    case 'Status':
      return msg('common.status', 'Status');
    case 'Created':
      return msg('common.created', 'Created');
    case 'Last updated':
      return msg('managedAgents.common.lastUpdated', 'Last updated');
    case 'Agent':
      return msg('managedAgents.common.agent', 'Agent');
    case 'Trigger':
      return msg('managedAgents.common.trigger', 'Trigger');
    case 'Type':
      return msg('analytics.table.type', 'Type');
    case 'Updated at':
      return msg('managedAgents.common.updatedAt', 'Updated at');
    case 'Actions':
      return msg('common.actions', 'Actions');
    case 'Auth':
      return msg('managedAgents.common.auth', 'Auth');
    case 'Payload':
      return msg('managedAgents.common.payload', 'Payload');
    default:
      return column;
  }
}

export const statusFilterOptions: Array<{ value: AgentStatusFilter; label: string }> = [
  { value: 'active', label: 'Active' },
  { value: 'all', label: 'All' },
];

export function dialogSubtitle(section: ManagedEntitySection) {
  switch (section) {
    case 'sessions':
      return 'Set up an instance of your agent in its environment.';
    case 'deployments':
      return 'Bind an agent to an environment so it can run manually or on a schedule.';
    case 'environments':
      return 'Create a reusable cloud container template for agent tools.';
    case 'credential-vaults':
      return 'Create a vault, then add credentials for MCP servers and other tools.';
    case 'memory-stores':
      return 'Create persistent memory that agents can use across sessions.';
  }
}
