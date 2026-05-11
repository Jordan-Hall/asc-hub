import $ from 'jquery';
import {createCodemirror} from './codemirror.ts';

export function initAuthorizedIntegrationClaimRuleEditor() {
  if (!$('.user.authorized-integrations').length) return;
  const _promise = createCodemirror($('#content')[0], 'claims.json', {language: 'JSON'});
}
