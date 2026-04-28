'use strict';
'require view';
'require ui';

var apiBase;

function apiCall(method, path, data) {
	return new Promise(function(resolve) {
		var xhr = new XMLHttpRequest();
		xhr.open(method, apiBase + path, true);
		if (data) xhr.setRequestHeader('Content-Type', 'application/json');
		xhr.onreadystatechange = function() {
			if (xhr.readyState === 4) {
				try { resolve(JSON.parse(xhr.responseText || '{}')); }
				catch (e) { resolve({ code: 500, message: '解析错误' }); }
			}
		};
		xhr.send(data ? JSON.stringify(data) : null);
	});
}

function apiGet(p) { return apiCall('GET', p, null); }
function apiPost(p, d) { return apiCall('POST', p, d); }
function apiPut(p, d) { return apiCall('PUT', p, d); }
function apiDelete(p) { return apiCall('DELETE', p, null); }

function showToast(msg) {
	var existing = document.querySelectorAll('.lynxdns-toast');
	existing.forEach(function(el) { el.remove(); });
	var t = E('div', { 'class': 'lynxdns-toast' }, msg);
	document.body.appendChild(t);
	setTimeout(function() { if (t.parentNode) t.remove(); }, 2500);
}

var ruleTypes = {
	remote: '远程解析',
	domestic: '国内解析',
	redirect: '重定向',
	block: '拦截'
};

var domainFormats = [
	{ prefix: '', label: '精确匹配', example: 'google.com', desc: '精确域名匹配' },
	{ prefix: '.', label: '后缀匹配', example: '.google.com', desc: '匹配域名及所有子域名' },
	{ prefix: '*.', label: '通配符', example: '*.google.com', desc: '通配符匹配' },
	{ prefix: 'regexp:', label: '正则表达式', example: 'regexp:^ads.*\\.com$', desc: '正则表达式匹配' },
	{ prefix: 'keyword:', label: '关键词', example: 'keyword:google', desc: '包含关键词即匹配' }
];

return view.extend({
	load: function() {
		apiBase = L.url('admin/services/lynxdns/api');
		return apiGet('/rules').catch(function() { return { code: 500 }; });
	},

	render: function(data) {
		if (!document.querySelector('link[href*="lynxdns.css"]')) {
			document.head.appendChild(E('link', { 'rel': 'stylesheet', 'type': 'text/css', 'href': '/luci-static/lynxdns.css' }));
		}
		var resp = data || {};
		var rawRules = (resp.code === 0 && resp.data) ? resp.data.rules : null;
		var rules = Array.isArray(rawRules) ? rawRules : [];

		var m = E('div', { 'class': 'cbi-map' }, [
			E('div', { 'class': 'cbi-map-descr' }, '管理自定义 DNS 路由规则，优先级高于 GeoSite/GeoIP 规则。')
		]);

		var priorityInfo = E('div', { 'class': 'priority-info', 'style': 'margin-top:20px' }, [
			E('strong', {}, '规则优先级（从高到低）：'),
			E('ol', {}, [
				E('li', {}, E('strong', { 'class': 'priority-color-remote' }, '自定义规则') + '（本页面） - 最高优先级'),
				E('li', {}, E('span', { 'class': 'priority-color-adblock' }, '广告过滤规则') + ' - 广告拦截分类'),
				E('li', {}, E('span', { 'class': 'priority-color-geosite' }, 'GeoSite 规则') + ' - 基于域名的路由'),
				E('li', {}, E('span', { 'class': 'priority-color-geoip' }, 'GeoIP 规则') + ' - 基于 IP 的路由'),
				E('li', {}, E('span', { 'class': 'priority-color-default' }, '默认策略') + ' - 最低优先级回退')
			])
		]);
		m.appendChild(priorityInfo);

		var tableContainer = E('div', { 'id': 'rules-table' });
		m.appendChild(tableContainer);

		var addBtn = E('button', {
			'class': 'cbi-button cbi-button-add important',
			'type': 'button',
			'style': 'margin-bottom:15px'
		}, '添加规则');
		addBtn.addEventListener('click', function() { showRuleModal(null, refreshTable); });
		m.appendChild(addBtn);

		function refreshTable() {
			apiGet('/rules').then(function(r) {
				var raw = (r.code === 0 && r.data) ? r.data.rules : null;
				rules = Array.isArray(raw) ? raw : [];
				renderTable();
			});
		}

		function renderTable() {
			while (tableContainer.firstChild) tableContainer.removeChild(tableContainer.firstChild);

			if (rules.length === 0) {
				tableContainer.appendChild(E('div', { 'class': 'cbi-section-descr' },
					'暂无自定义规则。点击「添加规则」创建新规则。'));
				return;
			}

			var table = E('table', { 'class': 'table', 'style': 'width:100%' }, [
				E('thead', {}, [
					E('tr', {}, [
						E('th', { 'style': 'width:40px' }, '#'),
						E('th', {}, '类型'),
						E('th', {}, '域名'),
						E('th', {}, '目标'),
						E('th', { 'style': 'width:80px' }, '启用'),
						E('th', { 'style': 'width:120px' }, '操作')
					])
				])
			]);

			var tbody = E('tbody');
			rules.forEach(function(rule, idx) {
				var typeLabel = ruleTypes[rule.type] || rule.type;
				var editBtn = E('button', {
					'class': 'cbi-button cbi-button-edit',
					'type': 'button',
					'style': 'margin-right:4px'
				}, '编辑');
				editBtn.addEventListener('click', function() { showRuleModal(rule, refreshTable); });

				var deleteBtn = E('button', {
					'class': 'cbi-button btn-rule-delete',
					'type': 'button',
					'style': 'margin-right:4px'
				}, '删除');
				deleteBtn.addEventListener('click', function(ev) {
					ev.preventDefault();
					ev.stopPropagation();
					if (confirm('确定要删除此规则吗？')) {
						apiCall('DELETE', '/rule?id=' + rule.id).then(function() { refreshTable(); });
					}
				});

				var tr = E('tr', {}, [
					E('td', {}, String(idx + 1)),
					E('td', {}, E('span', {
						'style': 'padding:2px 8px;border-radius:3px;font-size:12px;' +
							'background:' + getTypeColor(rule.type) + ';color:#fff'
					}, typeLabel)),
					E('td', { 'style': 'word-break:break-all' }, rule.domain),
					E('td', {}, rule.target || '-'),
					E('td', {}, createInlineToggle(rule)),
					E('td', {}, [editBtn, deleteBtn])
				]);
				tbody.appendChild(tr);
			});
			table.appendChild(tbody);
			tableContainer.appendChild(table);
		}

		function createInlineToggle(rule) {
			var cb = E('input', { 'type': 'checkbox', 'class': 'cbi-input-checkbox' });
			if (rule.enabled) cb.checked = true;
			cb.addEventListener('change', function() {
				apiPut('/rule?id=' + rule.id, { enabled: cb.checked }).then(function(r) {
					if (r.code !== 0) {
						cb.checked = !cb.checked;
						showToast('切换失败：' + (r.message || ''));
					}
				});
			});
			return cb;
		}

		function getTypeColor(type) {
			switch (type) {
				case 'remote': return '#2196F3';
				case 'domestic': return '#4CAF50';
				case 'redirect': return '#FF9800';
				case 'block': return '#f44336';
				default: return '#999';
			}
		}

		function showRuleModal(rule, onSave) {
			var isEdit = !!rule;
			var modal = E('div', { 'class': 'lynxdns-modal-overlay' });

			var form = E('div', { 'class': 'lynxdns-modal-content' }, [
				E('h3', { 'class': 'lynxdns-modal-title' }, isEdit ? '编辑规则' : '添加规则')
			]);

			var typeSelect = E('select', { 'class': 'cbi-input-select', 'style': 'width:100%' });
			Object.keys(ruleTypes).forEach(function(t) {
				var opt = E('option', { 'value': t }, ruleTypes[t]);
				if (rule && rule.type === t) opt.selected = true;
				typeSelect.appendChild(opt);
			});
			form.appendChild(E('div', { 'class': 'cbi-value' }, [
				E('label', { 'class': 'cbi-value-title' }, '规则类型'),
				E('div', { 'class': 'cbi-value-field' }, [typeSelect])
			]));

			var domainRow = E('div', { 'class': 'cbi-value' });
			domainRow.appendChild(E('label', { 'class': 'cbi-value-title' }, '域名'));

			var domainFieldDiv = E('div', { 'class': 'cbi-value-field' });

			var formatSelect = E('select', {
				'class': 'cbi-input-select',
				'style': 'width:100%;margin-bottom:4px'
			});
			domainFormats.forEach(function(f) {
				var opt = E('option', { 'value': f.prefix }, f.label + ' - ' + f.desc);
				if (rule && rule.domain && rule.domain.indexOf(f.prefix) === 0 && f.prefix) {
					opt.selected = true;
				} else if (rule && rule.domain && !rule.domain.startsWith('.') && !rule.domain.startsWith('*.') &&
						   !rule.domain.startsWith('regexp:') && !rule.domain.startsWith('keyword:') && f.prefix === '') {
					opt.selected = true;
				}
				formatSelect.appendChild(opt);
			});
			domainFieldDiv.appendChild(formatSelect);

			var domainInput = E('input', {
				'type': 'text', 'class': 'cbi-input-text',
				'value': rule ? rule.domain : '',
				'placeholder': '例如 google.com',
				'style': 'width:100%'
			});
			domainFieldDiv.appendChild(domainInput);

			var formatHelpDiv = E('div', { 'class': 'domain-format-help' }, [
				E('div', {}, [
					E('code', {}, 'google.com'), ' = 精确匹配 | ',
					E('code', {}, '.google.com'), ' = 后缀匹配 | ',
					E('code', {}, '*.google.com'), ' = 通配符'
				]),
				E('div', {}, [
					E('code', {}, 'regexp:^ads.*\\.com$'), ' = 正则 | ',
					E('code', {}, 'keyword:google'), ' = 关键词'
				])
			]);
			domainFieldDiv.appendChild(formatHelpDiv);

			domainRow.appendChild(domainFieldDiv);
			form.appendChild(domainRow);

			formatSelect.addEventListener('change', function() {
				var prefix = formatSelect.value;
				var example = '';
				domainFormats.forEach(function(f) {
					if (f.prefix === prefix) example = f.example;
				});
				domainInput.placeholder = '例如 ' + example;
			});

			var targetInput = E('input', {
				'type': 'text', 'class': 'cbi-input-text',
				'value': rule ? rule.target : '',
				'placeholder': '目标 IP 地址（仅重定向类型需要）',
				'style': 'width:100%'
			});
			var targetRow = E('div', { 'class': 'cbi-value', 'id': 'target-row' }, [
				E('label', { 'class': 'cbi-value-title' }, '目标地址'),
				E('div', { 'class': 'cbi-value-field' }, [targetInput])
			]);
			targetRow.style.display = (rule && rule.type === 'redirect') ? '' : 'none';
			form.appendChild(targetRow);

			typeSelect.addEventListener('change', function() {
				targetRow.style.display = (typeSelect.value === 'redirect') ? '' : 'none';
			});

			var enabledCb = E('input', { 'type': 'checkbox', 'class': 'cbi-input-checkbox' });
			if (!rule || rule.enabled) enabledCb.checked = true;
			form.appendChild(E('div', { 'class': 'cbi-value' }, [
				E('label', { 'class': 'cbi-value-title' }, '启用'),
				E('div', { 'class': 'cbi-value-field' }, [enabledCb])
			]));

			var btnRow = E('div', { 'class': 'lynxdns-modal-btn-row' });

			var cancelBtn = E('button', { 'class': 'cbi-button lynxdns-modal-btn-cancel', 'type': 'button' }, '取消');
			cancelBtn.addEventListener('click', function() { document.body.removeChild(modal); });
			btnRow.appendChild(cancelBtn);

			var saveBtn = E('button', { 'class': 'cbi-button lynxdns-modal-btn-save', 'type': 'button' }, '保存');
			saveBtn.addEventListener('click', function() {
				var rawDomain = domainInput.value.trim();
				if (!rawDomain) {
					showToast('域名不能为空。');
					return;
				}
				var prefix = formatSelect.value;
				var finalDomain = rawDomain;
				if (prefix && rawDomain.indexOf(prefix) !== 0) {
					finalDomain = prefix + rawDomain;
				}
				var ruleData = {
					type: typeSelect.value,
					domain: finalDomain,
					target: typeSelect.value === 'redirect' ? targetInput.value.trim() : '',
					enabled: enabledCb.checked
				};

				var promise;
				if (isEdit) {
					promise = apiPut('/rule?id=' + rule.id, ruleData);
				} else {
					promise = apiPost('/rules', ruleData);
				}

				promise.then(function(r) {
					if (r.code === 0) {
						document.body.removeChild(modal);
						onSave();
						showToast(isEdit ? '规则已更新。' : '规则已添加。');
					} else {
						showToast('操作失败：' + (r.message || '未知错误'));
					}
				});
			});
			btnRow.appendChild(saveBtn);
			form.appendChild(btnRow);
			modal.appendChild(form);
			document.body.appendChild(modal);
		}

		renderTable();
		return m;
	}
});
