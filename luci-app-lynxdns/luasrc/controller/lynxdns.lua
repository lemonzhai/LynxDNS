local json = require "luci.jsonc"

local PANEL_VERSION = "1.0.3"

module("luci.controller.lynxdns", package.seeall)

function index()
	entry({"admin", "services", "lynxdns", "api", "version"}, call("api_version")).leaf = true
	entry({"admin", "services", "lynxdns", "api", "status"}, call("api_status")).leaf = true
	entry({"admin", "services", "lynxdns", "api", "restart"}, call("api_restart")).leaf = true
	entry({"admin", "services", "lynxdns", "api", "config"}, call("api_config")).leaf = true
	entry({"admin", "services", "lynxdns", "api", "config_reload"}, call("api_config_reload")).leaf = true
	entry({"admin", "services", "lynxdns", "api", "rules"}, call("api_rules")).leaf = true
	entry({"admin", "services", "lynxdns", "api", "rule"}, call("api_rule")).leaf = true
	entry({"admin", "services", "lynxdns", "api", "rules_order"}, call("api_rules_order")).leaf = true
	entry({"admin", "services", "lynxdns", "api", "dns_stats"}, call("api_dns_stats")).leaf = true
	entry({"admin", "services", "lynxdns", "api", "dns_cache"}, call("api_dns_cache")).leaf = true
	entry({"admin", "services", "lynxdns", "api", "dns_lookup"}, call("api_dns_lookup")).leaf = true
	entry({"admin", "services", "lynxdns", "api", "geo_status"}, call("api_geo_status")).leaf = true
	entry({"admin", "services", "lynxdns", "api", "geo_update"}, call("api_geo_update")).leaf = true
	entry({"admin", "services", "lynxdns", "api", "geo_sources"}, call("api_geo_sources")).leaf = true
	entry({"admin", "services", "lynxdns", "api", "geo_progress"}, call("api_geo_progress")).leaf = true
	entry({"admin", "services", "lynxdns", "api", "geo_history"}, call("api_geo_history")).leaf = true
	entry({"admin", "services", "lynxdns", "api", "service"}, call("api_service")).leaf = true
	entry({"admin", "services", "lynxdns", "api", "log_read"}, call("api_log_read")).leaf = true
	entry({"admin", "services", "lynxdns", "api", "ws_config"}, call("api_ws_config")).leaf = true
	entry({"admin", "services", "lynxdns", "api", "panel_info"}, call("api_panel_info")).leaf = true
	entry({"admin", "services", "lynxdns", "api", "log_retention"}, call("api_log_retention")).leaf = true
	entry({"admin", "services", "lynxdns", "api", "log_clear"}, call("api_log_clear")).leaf = true
	entry({"admin", "services", "lynxdns", "api", "query_history"}, call("api_query_history")).leaf = true
end

function api_query_history()
	local http = require "luci.http"
	local limit = tonumber(http.formvalue("limit") or "100")
	local resp = core_api_call("GET", "/queries/recent?limit=" .. tostring(limit))
	if resp then
		json_response(resp)
	else
		json_response({ code = 500, message = "failed to fetch query history" })
	end
end

function get_api_config()
	local addr = "127.0.0.1"
	local port = "5335"
	local secret = ""

	local fs = require "nixio.fs"
	local content = fs.readfile("/etc/lynxdns/config.yaml")
	if content then
		local api_section = content:match("\napi:(.-)\n%a")
		if not api_section then
			api_section = content:match("\napi:(.*)")
		end
		if api_section then
			local a = api_section:match("addr:%s*[\"']([^\"']+)[\"']")
			if not a then a = api_section:match("addr:%s*(%S+)") end
			if a then addr = a end

			local p = api_section:match("port:%s*(%d+)")
			if p then port = p end

			secret = api_section:match("secret:%s*[\"']([^\"']*)[\"']")
			if secret == nil then
				secret = api_section:match("secret:%s*(%S+)")
			end
			if not secret then secret = "" end
		end
	end

	return {
		addr = addr,
		port = port,
		secret = secret
	}
end

function core_api_call(method, path, data)
	local api = get_api_config()
	local url = string.format("http://%s:%s/api/v1%s", api.addr, api.port, path)

	local cmd = string.format("curl -s -m 5 -X %s", method)

	if api.secret and api.secret ~= "" then
		cmd = cmd .. string.format(' -H "Authorization: Bearer %s"', api.secret)
	end

	if data then
		cmd = cmd .. ' -H "Content-Type: application/json"'
		local json_str = json.stringify(data)
		json_str = json_str:gsub("'", "'\\''")
		cmd = cmd .. string.format(" -d '%s'", json_str)
	end

	cmd = cmd .. string.format(" '%s' 2>/dev/null", url)

	local handle = io.popen(cmd)
	local result = handle:read("*a")
	handle:close()

	if not result or result == "" then
		return { code = 500, message = "failed to connect to LynxDNS core", data = nil }
	end

	local parsed = json.parse(result)
	if not parsed then
		return { code = 500, message = "invalid response from LynxDNS core", data = nil }
	end

	return parsed
end

function json_response(data)
	local http = require "luci.http"
	http.prepare_content("application/json")
	http.write_json(data)
end

function read_request_body()
	local http = require "luci.http"
	local body = http.content()
	if body and body ~= "" then
		return json.parse(body)
	end
	return nil
end

function api_version()
	json_response(core_api_call("GET", "/version"))
end

function api_status()
	json_response(core_api_call("GET", "/status"))
end

function api_restart()
	json_response(core_api_call("POST", "/restart"))
end

function api_config()
	local http = require "luci.http"
	local method = http.getenv("REQUEST_METHOD")

	if method == "GET" then
		local resp = core_api_call("GET", "/config")
		if resp and resp.code == 0 and resp.data and resp.data.api then
			local api_cfg = get_api_config()
			resp.data.api.secret = api_cfg.secret
		end
		json_response(resp)
	elseif method == "PATCH" then
		local data = read_request_body()
		json_response(core_api_call("PATCH", "/config", data))
	else
		http.status(405, "Method Not Allowed")
		json_response({ code = 405, message = "method not allowed" })
	end
end

function api_config_reload()
	local http = require "luci.http"
	local method = http.getenv("REQUEST_METHOD")

	if method == "POST" then
		json_response(core_api_call("POST", "/config/reload"))
	else
		http.status(405, "Method Not Allowed")
		json_response({ code = 405, message = "method not allowed" })
	end
end

function api_rules()
	local http = require "luci.http"
	local method = http.getenv("REQUEST_METHOD")

	if method == "GET" then
		json_response(core_api_call("GET", "/rules"))
	elseif method == "POST" then
		local data = read_request_body()
		json_response(core_api_call("POST", "/rules", data))
	else
		http.status(405, "Method Not Allowed")
		json_response({ code = 405, message = "method not allowed" })
	end
end

function api_rule()
	local http = require "luci.http"
	local method = http.getenv("REQUEST_METHOD")
	local id = http.formvalue("id")

	if not id or id == "" then
		json_response({ code = 400, message = "rule id is required" })
		return
	end

	if method == "PUT" then
		local data = read_request_body()
		json_response(core_api_call("PUT", "/rules/" .. id, data))
	elseif method == "DELETE" then
		json_response(core_api_call("DELETE", "/rules/" .. id))
	else
		http.status(405, "Method Not Allowed")
		json_response({ code = 405, message = "method not allowed" })
	end
end

function api_rules_order()
	local data = read_request_body()
	json_response(core_api_call("POST", "/rules/order", data))
end

function api_dns_stats()
	json_response(core_api_call("GET", "/dns/stats"))
end

function api_dns_cache()
	local http = require "luci.http"
	local method = http.getenv("REQUEST_METHOD")

	if method == "GET" then
		local page = http.formvalue("page") or "1"
		local page_size = http.formvalue("page_size") or "20"
		local domain = http.formvalue("domain") or ""
		local path = string.format("/dns/cache?page=%s&page_size=%s", page, page_size)
		if domain ~= "" then
			path = path .. "&domain=" .. domain
		end
		json_response(core_api_call("GET", path))
	elseif method == "DELETE" then
		json_response(core_api_call("DELETE", "/dns/cache"))
	else
		http.status(405, "Method Not Allowed")
		json_response({ code = 405, message = "method not allowed" })
	end
end

function api_dns_lookup()
	local data = read_request_body()
	json_response(core_api_call("POST", "/dns/lookup", data))
end

function api_geo_status()
	json_response(core_api_call("GET", "/geo/status"))
end

function api_geo_update()
	local data = read_request_body()
	json_response(core_api_call("POST", "/geo/update", data))
end

function api_geo_sources()
	json_response(core_api_call("GET", "/geo/sources"))
end

function api_geo_progress()
	local http = require "luci.http"
	local target = http.formvalue("target") or ""
	local path = "/geo/progress"
	if target ~= "" then
		path = path .. "?target=" .. target
	end
	json_response(core_api_call("GET", path))
end

function api_geo_history()
	local http = require "luci.http"
	local limit = http.formvalue("limit") or "20"
	json_response(core_api_call("GET", "/geo/history?limit=" .. limit))
end

function api_service()
	local http = require "luci.http"
	local sys = require "luci.sys"
	local uci = require("luci.model.uci").cursor()
	local data = read_request_body()

	if data and data.enabled ~= nil then
		if data.enabled then
			uci:set("lynxdns", "main", "enabled", "1")
			uci:commit("lynxdns")
			sys.call("/etc/init.d/lynxdns enable >/dev/null 2>&1")
			sys.call("/etc/init.d/lynxdns start >/dev/null 2>&1")
		else
			uci:set("lynxdns", "main", "enabled", "0")
			uci:commit("lynxdns")
			sys.call("/etc/init.d/lynxdns stop >/dev/null 2>&1")
			sys.call("/etc/init.d/lynxdns disable >/dev/null 2>&1")
		end
		json_response({ code = 0, message = "success" })
	else
		local running = sys.call("pidof lynxdns >/dev/null 2>&1") == 0
		local enabled = (uci:get("lynxdns", "main", "enabled") or "0") == "1"
		json_response({ code = 0, message = "success", data = { running = running, enabled = enabled } })
	end
end

function get_log_file_path()
	local config_resp = core_api_call("GET", "/config")
	if config_resp and config_resp.code == 0 and config_resp.data and config_resp.data.log and config_resp.data.log.file then
		return config_resp.data.log.file
	end
	return "/var/log/lynxdns.log"
end

function api_log_read()
	local http = require "luci.http"
	local fs = require "nixio.fs"
	local sys = require "luci.sys"
	local lines = tonumber(http.formvalue("lines") or "200")
	local log_file = get_log_file_path()

	local content = fs.readfile(log_file)

	-- Check if logs were recently cleared
	local cleared_marker = "/var/log/lynxdns.cleared"
	local cleared_time = 0
	if fs.access(cleared_marker) then
		local marker_content = fs.readfile(cleared_marker)
		if marker_content and marker_content ~= "" then
			cleared_time = tonumber(marker_content) or 0
		end
	end

	if not content or content == "" then
		-- Only use syslog fallback if no recent clear, or filter by timestamp
		local handle
		if cleared_time > 0 then
			-- Get current syslog, then filter by timestamp (logs newer than cleared_time)
			handle = io.popen("logread -e lynxdns 2>/dev/null")
			if handle then
				local syslog_content = handle:read("*a")
				handle:close()
				-- Filter logs: keep only entries after the cleared timestamp
				local filtered_lines = {}
				for line in syslog_content:gmatch("[^\n]+") do
					-- OpenWrt log format: "Mon Jan 2 15:04:05 2026 daemon.info lynxdns[123]: message"
					-- Try to extract timestamp and compare
					local log_time = parse_logread_timestamp(line)
					if log_time == 0 or log_time > cleared_time then
						filtered_lines[#filtered_lines + 1] = line
					end
				end
				if #filtered_lines > 0 then
					-- Take only the last N lines
					local start_idx = math.max(1, #filtered_lines - lines + 1)
					content = ""
					for i = start_idx, #filtered_lines do
						content = content .. filtered_lines[i] .. "\n"
					end
				else
					content = ""
				end
			end
		else
			handle = io.popen("logread -e lynxdns 2>/dev/null | tail -n " .. tostring(lines))
			if handle then
				content = handle:read("*a")
				handle:close()
			end
		end
	end

	if not content or content == "" then
		local handle
		if cleared_time > 0 then
			handle = io.popen("logread 2>/dev/null")
			if handle then
				local syslog_content = handle:read("*a")
				handle:close()
				local filtered_lines = {}
				for line in syslog_content:gmatch("[^\n]+") do
					if line:lower():find("lynxdns") then
						local log_time = parse_logread_timestamp(line)
						if log_time == 0 or log_time > cleared_time then
							filtered_lines[#filtered_lines + 1] = line
						end
					end
				end
				if #filtered_lines > 0 then
					local start_idx = math.max(1, #filtered_lines - lines + 1)
					content = ""
					for i = start_idx, #filtered_lines do
						content = content .. filtered_lines[i] .. "\n"
					end
				else
					content = ""
				end
			end
		else
			handle = io.popen("logread 2>/dev/null | grep -i lynxdns | tail -n " .. tostring(lines))
			if handle then
				content = handle:read("*a")
				handle:close()
			end
		end
	end

	if not content or content == "" then
		json_response({ code = 0, message = "success", data = { logs = "暂无日志。请确认 LynxDNS 服务正在运行，且日志输出已启用。", lines = 0, total = 0 } })
		return
	end

	local log_lines = {}
	for line in content:gmatch("[^\n]+") do
		log_lines[#log_lines + 1] = line
	end

	local total = #log_lines
	local start_idx = math.max(1, total - lines + 1)
	local result = {}
	for i = start_idx, total do
		result[#result + 1] = log_lines[i]
	end

	json_response({
		code = 0,
		message = "success",
		data = {
			logs = table.concat(result, "\n"),
			lines = #result,
			total = total
		}
	})
end

-- Parse logread timestamp and return Unix timestamp
-- OpenWrt logread format: "Mon Jan 2 15:04:05 2026 daemon.info lynxdns[123]: message"
function parse_logread_timestamp(line)
	-- Match the date/time prefix pattern
	local month_map = {
		Jan = 1, Feb = 2, Mar = 3, Apr = 4, May = 5, Jun = 6,
		Jul = 7, Aug = 8, Sep = 9, Oct = 10, Nov = 11, Dec = 12
	}

	-- Pattern: "Mon Jan  2 15:04:05 2026"
	local weekday, month_str, day, time_str, year = line:match("^(%a%a%a)%s+(%a%a%a)%s+(%d+)%s+(%d%d:%d%d:%d%d)%s+(%d%d%d%d)")
	if not weekday or not month_str then
		return 0
	end

	local month = month_map[month_str]
	if not month then
		return 0
	end

	local hour, min, sec = time_str:match("(%d%d):(%d%d):(%d%d)")
	if not hour then
		return 0
	end

	-- Convert to Unix timestamp (simple approximation, ignoring timezone)
	-- This is good enough for relative comparison (before/after clear)
	local year_num = tonumber(year) or 2026
	local day_num = tonumber(day) or 1
	local hour_num = tonumber(hour) or 0
	local min_num = tonumber(min) or 0
	local sec_num = tonumber(sec) or 0

	-- Simple Unix timestamp calculation
	local days = 0
	for y = 1970, year_num - 1 do
		if (y % 4 == 0 and y % 100 ~= 0) or (y % 400 == 0) then
			days = days + 366
		else
			days = days + 365
		end
	end
	for m = 1, month - 1 do
		if m == 2 then
			if (year_num % 4 == 0 and year_num % 100 ~= 0) or (year_num % 400 == 0) then
				days = days + 29
			else
				days = days + 28
			end
		elseif m == 4 or m == 6 or m == 9 or m == 11 then
			days = days + 30
		else
			days = days + 31
		end
	end
	days = days + day_num - 1

	return days * 86400 + hour_num * 3600 + min_num * 60 + sec_num
end

function api_ws_config()
	local api = get_api_config()
	local ws_host = api.addr
	if ws_host == "127.0.0.1" or ws_host == "localhost" or ws_host == "0.0.0.0" then
		local http = require "luci.http"
		local server_name = http.getenv("SERVER_NAME")
		if server_name and server_name ~= "" then
			ws_host = server_name
		end
	end
	json_response({
		code = 0,
		message = "success",
		data = {
			host = ws_host,
			port = api.port,
			secret = api.secret
		}
	})
end

function api_panel_info()
	json_response({
		code = 0,
		message = "success",
		data = {
			version = PANEL_VERSION
		}
	})
end

function api_log_retention()
	local http = require "luci.http"
	local uci = require("luci.model.uci").cursor()
	local method = http.getenv("REQUEST_METHOD")

	if method == "GET" then
		local seconds = tonumber(uci:get("lynxdns", "log", "retention") or "604800")
		json_response({ code = 0, message = "success", data = { seconds = seconds } })
	elseif method == "POST" then
		local data = read_request_body()
		if data and data.seconds then
			if not uci:get("lynxdns", "log") then
				uci:set("lynxdns", "log", "log")
			end
			uci:set("lynxdns", "log", "retention", tostring(math.floor(tonumber(data.seconds) or 604800)))
			uci:commit("lynxdns")
			json_response({ code = 0, message = "success" })
		else
			json_response({ code = 400, message = "seconds is required" })
		end
	else
		http.status(405, "Method Not Allowed")
		json_response({ code = 405, message = "method not allowed" })
	end
end

function api_log_clear()
	local http = require "luci.http"
	local method = http.getenv("REQUEST_METHOD")

	if method == "DELETE" then
		local log_file = get_log_file_path()
		local fs = require "nixio.fs"

		-- Clear the log file
		if fs.access(log_file) then
			local f = io.open(log_file, "w")
			if f then f:close() end
		end

		-- Write cleared timestamp marker to filter syslog fallback
		local cleared_marker = "/var/log/lynxdns.cleared"
		local current_time = os.time()
		local marker_file = io.open(cleared_marker, "w")
		if marker_file then
			marker_file:write(tostring(current_time))
			marker_file:close()
		end

		json_response({ code = 0, message = "success", data = { cleared = true, cleared_at = current_time } })
	else
		http.status(405, "Method Not Allowed")
		json_response({ code = 405, message = "method not allowed" })
	end
end
