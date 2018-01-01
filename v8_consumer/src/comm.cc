// Copyright (c) 2017 Couchbase, Inc.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//     http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an "AS IS"
// BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express
// or implied. See the License for the specific language governing
// permissions and limitations under the License.

#include "utils.h"

CURLClient::CURLClient() : headers(nullptr) { curl_handle = curl_easy_init(); }

CURLClient::~CURLClient() { curl_easy_cleanup(curl_handle); }

// Callback gets invoked for every chunk of body data that arrives
size_t CURLClient::BodyCallback(void *buffer, size_t size, size_t nmemb,
                                void *cookie) {
  auto realsize = size * nmemb;
  auto data = static_cast<std::string *>(cookie);
  auto content = static_cast<char *>(buffer);
  data->assign(&content[0], &content[0] + realsize);
  return realsize;
}

// Callback gets invoked for every header that arrives
size_t CURLClient::HeaderCallback(char *buffer, size_t size, size_t nitems,
                                  void *cookie) {
  auto realsize = size * nitems;
  auto headers =
      static_cast<std::unordered_map<std::string, std::string> *>(cookie);
  auto header = std::string(static_cast<char *>(buffer));

  // Split the header into key:value
  auto find = header.find(':');
  if (find != std::string::npos) {
    (*headers)[header.substr(0, find)] =
        header.substr(find + 1); // Adding 1 to discount the ':'
  }

  return realsize;
}

CURLResponse CURLClient::HTTPPost(const std::vector<std::string> &header_list,
                                  const std::string &url,
                                  const std::string &body) {
  CURLResponse response;

  code = curl_easy_setopt(curl_handle, CURLOPT_URL, url.c_str());
  if (code != CURLE_OK) {
    response.is_error = true;
    response.response =
        "Unable to set URL: " + std::string(curl_easy_strerror(code));
    return response;
  }

  for (const auto &header : header_list) {
    headers = curl_slist_append(headers, header.c_str());
  }

  code = curl_easy_setopt(curl_handle, CURLOPT_HTTPHEADER, headers);
  if (code != CURLE_OK) {
    response.is_error = true;
    response.response = "Unable to do set HTTP header(s): " +
                        std::string(curl_easy_strerror(code));
    return response;
  }

  code = curl_easy_setopt(curl_handle, CURLOPT_POSTFIELDS, body.c_str());
  if (code != CURLE_OK) {
    response.is_error = true;
    response.response =
        "Unable to set POST body: " + std::string(curl_easy_strerror(code));
    return response;
  }

  code = curl_easy_setopt(curl_handle, CURLOPT_WRITEFUNCTION,
                          CURLClient::BodyCallback);
  if (code != CURLE_OK) {
    response.is_error = true;
    response.response = "Unable to set body callback function: " +
                        std::string(curl_easy_strerror(code));
    return response;
  }

  code = curl_easy_setopt(curl_handle, CURLOPT_HEADERFUNCTION,
                          CURLClient::HeaderCallback);
  if (code != CURLE_OK) {
    response.is_error = true;
    response.response = "Unable to set header callback function: " +
                        std::string(curl_easy_strerror(code));
    return response;
  }

  code = curl_easy_setopt(curl_handle, CURLOPT_HEADERDATA,
                          (void *)&response.headers);
  if (code != CURLE_OK) {
    response.is_error = true;
    response.response = "Unable to set cookie for headers: " +
                        std::string(curl_easy_strerror(code));
    return response;
  }

  code = curl_easy_setopt(curl_handle, CURLOPT_WRITEDATA,
                          (void *)&response.response);
  if (code != CURLE_OK) {
    response.is_error = true;
    response.response = "Unable to set cookie for body: " +
                        std::string(curl_easy_strerror(code));
    return response;
  }

  code = curl_easy_setopt(curl_handle, CURLOPT_USERAGENT, "libcurl-agent/1.0");
  if (code != CURLE_OK) {
    response.is_error = true;
    response.response =
        "Unable to set user agent: " + std::string(curl_easy_strerror(code));
    return response;
  }

  code = curl_easy_setopt(curl_handle, CURLOPT_TIMEOUT, 30L);
  if (code != CURLE_OK) {
    response.is_error = true;
    response.response = "Unable to set timeout";
    return response;
  }

  code = curl_easy_perform(curl_handle);
  if (code != CURLE_OK) {
    response.is_error = true;
    response.response =
        "Unable to do HTTP POST: " + std::string(curl_easy_strerror(code));
    return response;
  }

  response.is_error = false;
  return response;
}

Communicator::Communicator(const std::string &host_ip,
                           const std::string &host_port, v8::Isolate *isolate)
    : isolate(isolate) {
  parse_query_url = "http://" + host_ip + ":" + host_port + "/parseQuery";
  get_creds_url = "http://" + host_ip + ":" + host_port + "/getCreds";
  get_named_params_url =
      "http://" + host_ip + ":" + host_port + "/getNamedParams";
}

CredsInfo Communicator::GetCreds(const std::string &endpoint) {
  v8::HandleScope handle_scope(isolate);

  auto context = v8::Context::New(isolate);
  v8::Context::Scope context_scope(context);

  CURLClient curl;
  auto response =
      curl.HTTPPost({"Content-Type: text/plain"}, get_creds_url, endpoint);

  CredsInfo info;
  info.is_error = response.is_error;
  if (response.is_error) {
    info.error = response.response;
    return info;
  }

  if (std::stoi(response.headers["Status"]) != 0) {
    info.is_error = true;
    info.error = response.response;
    return info;
  }

  auto response_obj =
      v8::JSON::Parse(v8Str(isolate, response.response))->ToObject();
  auto username_v8_str = response_obj->Get(v8Str(isolate, "username"));
  auto password_v8_str = response_obj->Get(v8Str(isolate, "password"));
  v8::String::Utf8Value username_utf8(username_v8_str);
  v8::String::Utf8Value password_utf8(password_v8_str);

  info.username = *username_utf8;
  info.password = *password_utf8;
  return info;
}

ParseInfo Communicator::ParseQuery(const std::string &query) {
  v8::HandleScope handle_scope(isolate);

  CURLClient curl;
  auto response =
      curl.HTTPPost({"Content-Type: text/plain"}, parse_query_url, query);

  ParseInfo info;
  info.is_valid = false;
  info.info = "Something went wrong while parsing the N1QL query";

  if (response.is_error) {
    LOG(logError)
        << "Unable to parse N1QL query: Something went wrong with CURL lib: "
        << response.response << std::endl;
    return info;
  }

  if (response.headers.find("Status") == response.headers.end()) {
    LOG(logError)
        << "Unable to parse N1QL query: status code is missing in header:"
        << response.response << std::endl;
    return info;
  }

  int status = std::stoi(response.headers["Status"]);
  if (status != 0) {
    LOG(logError) << "Unable to parse N1QL query: non-zero status in header"
                  << status << std::endl;
    return info;
  }

  auto resp_obj =
      v8::JSON::Parse(v8Str(isolate, response.response)).As<v8::Object>();
  if (resp_obj.IsEmpty()) {
    LOG(logError) << "Unable to cast response to JSON" << std::endl;
    return info;
  }

  return ExtractParseInfo(resp_obj);
}

NamedParamsInfo Communicator::GetNamedParams(const std::string &query) {
  CURLClient curl;
  auto response =
      curl.HTTPPost({"Content-Type: text/plain"}, get_named_params_url, query);

  NamedParamsInfo info;
  info.p_info.is_valid = false;
  info.p_info.info = "Something went wrong while extracting named parameters";

  if (response.is_error) {
    LOG(logError)
        << "Unable to get named params: Something went wrong with CURL lib: "
        << response.response << std::endl;
    return info;
  }

  if (response.headers.find("Status") == response.headers.end()) {
    LOG(logError)
        << "Unable to get named params: status code is missing in header: "
        << response.response << std::endl;
    info.p_info.info = response.response;
    return info;
  }

  if (std::stoi(response.headers["Status"]) != 0) {
    LOG(logError) << "Unable to get named params: non-zero status in header: "
                  << response.response << std::endl;
    return info;
  }

  auto resp_obj =
      v8::JSON::Parse(v8Str(isolate, response.response)).As<v8::Object>();
  if (resp_obj.IsEmpty()) {
    LOG(logError) << "Unable to get named params: unable to parse JSON"
                  << std::endl;
    return info;
  }

  auto p_info_v8obj = resp_obj->Get(v8Str(isolate, "p_info")).As<v8::Object>();
  auto named_params_v8arr =
      resp_obj->Get(v8Str(isolate, "named_params")).As<v8::Array>();
  info.p_info = ExtractParseInfo(p_info_v8obj);

  for (int i = 0; i < named_params_v8arr->Length(); ++i) {
    v8::String::Utf8Value named_param_utf8(
        named_params_v8arr->Get(static_cast<uint32_t>(i)));
    info.named_params.emplace_back(*named_param_utf8);
  }

  return info;
}

ParseInfo
Communicator::ExtractParseInfo(v8::Local<v8::Object> &parse_info_v8val) {
  v8::HandleScope handle_scope(isolate);

  ParseInfo info;
  auto is_valid_v8val = parse_info_v8val->Get(v8Str(isolate, "is_valid"));
  auto info_v8val = parse_info_v8val->Get(v8Str(isolate, "info"));

  info.is_valid = is_valid_v8val->ToBoolean()->Value();
  v8::String::Utf8Value info_str(info_v8val);
  info.info = *info_str;
  return info;
}