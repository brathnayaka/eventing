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

#include <atomic>
#include <chrono>
#include <cstdlib>
#include <cstring>
#include <ctime>
#include <iostream>
#include <mutex>
#include <regex>
#include <sstream>
#include <thread>
#include <typeinfo>
#include <unistd.h>

#include "../include/bucket.h"
#include "../include/n1ql.h"
#include "../include/parse_deployment.h"

#define MAXPATHLEN 256
#define TRANSPILER_JS_PATH "transpiler.js"
#define ESTOOLS_PATH "estools.js"

N1QL *n1ql_handle;

enum RETURN_CODE {
  SUCCESS = 0,
  FAILED_TO_COMPILE_JS,
  NO_HANDLERS_DEFINED,
  FAILED_INIT_BUCKET_HANDLE,
  FAILED_INIT_N1QL_HANDLE,
  ON_UPDATE_CALL_FAIL,
  ON_DELETE_CALL_FAIL
};

// Copies a C string to a 16-bit string.  Does not check for buffer overflow.
// Does not use the V8 engine to convert strings, so it can be used
// in any thread.  Returns the length of the string.
int AsciiToUtf16(const char *input_buffer, uint16_t *output_buffer) {
  int i;
  for (i = 0; input_buffer[i] != '\0'; ++i) {
    // ASCII does not use chars > 127, but be careful anyway.
    output_buffer[i] = static_cast<unsigned char>(input_buffer[i]);
  }
  output_buffer[i] = 0;
  return i;
}

// Reads a file from the given path and returns the content.
std::string ReadFile(std::string file_path) {
  std::ifstream file(file_path);
  std::string source((std::istreambuf_iterator<char>(file)),
                     std::istreambuf_iterator<char>());
  return source;
}

v8::Local<v8::String> createUtf8String(v8::Isolate *isolate, const char *str) {
  return v8::String::NewFromUtf8(isolate, str, v8::NewStringType::kNormal)
      .ToLocalChecked();
}

std::string ObjectToString(v8::Local<v8::Value> value) {
  v8::String::Utf8Value utf8_value(value);
  return std::string(*utf8_value);
}

std::string ToString(v8::Isolate *isolate, v8::Handle<v8::Value> object) {
  v8::HandleScope handle_scope(isolate);

  v8::Local<v8::Context> context = isolate->GetCurrentContext();
  v8::Local<v8::Object> global = context->Global();

  v8::Local<v8::Object> JSON =
      global->Get(v8::String::NewFromUtf8(isolate, "JSON"))->ToObject();
  v8::Local<v8::Function> JSON_stringify = v8::Local<v8::Function>::Cast(
      JSON->Get(v8::String::NewFromUtf8(isolate, "stringify")));

  v8::Local<v8::Value> result;
  v8::Local<v8::Value> args[1];
  args[0] = {object};
  result = JSON_stringify->Call(context->Global(), 1, args);
  return ObjectToString(result);
}

lcb_t *UnwrapLcbInstance(v8::Local<v8::Object> obj) {
  v8::Local<v8::External> field =
      v8::Local<v8::External>::Cast(obj->GetInternalField(1));
  void *ptr = field->Value();
  return static_cast<lcb_t *>(ptr);
}

lcb_t *UnwrapV8WorkerLcbInstance(v8::Local<v8::Object> obj) {
  v8::Local<v8::External> field =
      v8::Local<v8::External>::Cast(obj->GetInternalField(2));
  void *ptr = field->Value();
  return static_cast<lcb_t *>(ptr);
}

V8Worker *UnwrapV8WorkerInstance(v8::Local<v8::Object> obj) {
  v8::Local<v8::External> field =
      v8::Local<v8::External>::Cast(obj->GetInternalField(1));
  void *ptr = field->Value();
  return static_cast<V8Worker *>(ptr);
}

std::map<std::string, std::string> *UnwrapMap(v8::Local<v8::Object> obj) {
  v8::Local<v8::External> field =
      v8::Local<v8::External>::Cast(obj->GetInternalField(0));
  void *ptr = field->Value();
  return static_cast<std::map<std::string, std::string> *>(ptr);
}

// Extracts a C string from a V8 Utf8Value.
const char *ToCString(const v8::String::Utf8Value &value) {
  return *value ? *value : "<std::string conversion failed>";
}

const char *ToJson(v8::Isolate *isolate, v8::Handle<v8::Value> object) {
  v8::HandleScope handle_scope(isolate);

  v8::Local<v8::Context> context = isolate->GetCurrentContext();
  v8::Local<v8::Object> global = context->Global();

  v8::Local<v8::Object> JSON =
      global->Get(v8::String::NewFromUtf8(isolate, "JSON"))->ToObject();
  v8::Local<v8::Function> JSON_stringify = v8::Local<v8::Function>::Cast(
      JSON->Get(v8::String::NewFromUtf8(isolate, "stringify")));

  v8::Local<v8::Value> result;
  v8::Local<v8::Value> args[1];
  args[0] = {object};
  result = JSON_stringify->Call(context->Global(), 1, args);
  v8::String::Utf8Value str(result->ToString());
  return ToCString(str);
}

void Print(const v8::FunctionCallbackInfo<v8::Value> &args) {
  std::string log_msg;
  for (int i = 0; i < args.Length(); i++) {
    log_msg += ToJson(args.GetIsolate(), args[i]);
    log_msg += ' ';
  }
  LOG(logDebug) << log_msg << '\n';
}

std::string ConvertToISO8601(std::string timestamp) {
  char buf[sizeof "2016-08-09T10:11:12"];
  std::string buf_s;
  time_t now;

  int timerValue = atoi(timestamp.c_str());

  // Expiry timers more than 30 days will mention epoch
  // otherwise it will mention seconds from when key
  // was set
  if (timerValue > 25920000) {
    now = timerValue;
    strftime(buf, sizeof buf, "%FT%T", gmtime(&now));
    buf_s.assign(buf);
  } else {
    time(&now);
    now += timerValue;
    strftime(buf, sizeof buf, "%FT%T", gmtime(&now));
    buf_s.assign(buf);
  }
  return buf_s;
}

// Exception details will be appended to the first argument.
std::string ExceptionString(v8::Isolate *isolate, v8::TryCatch *try_catch) {
  std::string out;
  size_t scratchSize = 20;
  char scratch[scratchSize]; // just some scratch space for sprintf

  v8::HandleScope handle_scope(isolate);
  v8::String::Utf8Value exception(try_catch->Exception());
  const char *exception_string = ToCString(exception);

  v8::Handle<v8::Message> message = try_catch->Message();

  if (message.IsEmpty()) {
    // V8 didn't provide any extra information about this error; just
    // print the exception.
    out.append(exception_string);
    out.append("\n");
  } else {
    // Print (filename):(line number)
    v8::String::Utf8Value filename(message->GetScriptOrigin().ResourceName());
    const char *filename_string = ToCString(filename);
    int linenum = message->GetLineNumber();

    snprintf(scratch, scratchSize, "%i", linenum);
    out.append(filename_string);
    out.append(":");
    out.append(scratch);
    out.append("\n");

    // Print line of source code.
    v8::String::Utf8Value sourceline(message->GetSourceLine());
    const char *sourceline_string = ToCString(sourceline);

    out.append(sourceline_string);
    out.append("\n");

    // Print wavy underline (GetUnderline is deprecated).
    int start = message->GetStartColumn();
    for (int i = 0; i < start; i++) {
      out.append(" ");
    }
    int end = message->GetEndColumn();
    for (int i = start; i < end; i++) {
      out.append("^");
    }
    out.append("\n");
    v8::String::Utf8Value stack_trace(try_catch->StackTrace());
    if (stack_trace.length() > 0) {
      const char *stack_trace_string = ToCString(stack_trace);
      out.append(stack_trace_string);
      out.append("\n");
    } else {
      out.append(exception_string);
      out.append("\n");
    }
  }
  return out;
}

std::vector<std::string> &split(const std::string &s, char delim,
                                std::vector<std::string> &elems) {
  std::stringstream ss(s);
  std::string item;
  while (getline(ss, item, delim)) {
    elems.push_back(item);
  }
  return elems;
}

std::vector<std::string> split(const std::string &s, char delim) {
  std::vector<std::string> elems;
  split(s, delim, elems);
  return elems;
}

static void op_get_callback(lcb_t instance, int cbtype,
                            const lcb_RESPBASE *rb) {
  const lcb_RESPGET *resp = reinterpret_cast<const lcb_RESPGET *>(rb);
  Result *result = reinterpret_cast<Result *>(rb->cookie);

  result->status = resp->rc;
  result->cas = resp->cas;
  result->itmflags = resp->itmflags;
  result->value.clear();

  if (resp->rc == LCB_SUCCESS) {
    result->value.assign(reinterpret_cast<const char *>(resp->value),
                         resp->nvalue);
  } else {
    LOG(logError) << "lcb get failed with error "
                  << lcb_strerror(instance, resp->rc) << '\n';
  }
}

static void op_set_callback(lcb_t instance, int cbtype,
                            const lcb_RESPBASE *rb) {
  LOG(logTrace) << "lcb set response code: " << lcb_strerror(instance, rb->rc)
                << '\n';
}

static ArrayBufferAllocator array_buffer_allocator;

V8Worker::V8Worker(std::string app_name, std::string dep_cfg,
                   std::string kv_host_port, std::string rbac_user,
                   std::string rbac_pass) {
  v8::V8::InitializeICU();
  v8::Platform *platform = v8::platform::CreateDefaultPlatform();
  v8::V8::InitializePlatform(platform);
  v8::V8::Initialize();

  v8::Isolate::CreateParams create_params;
  create_params.array_buffer_allocator = &allocator;

  isolate_ = v8::Isolate::New(create_params);
  v8::Locker locker(isolate_);
  v8::Isolate::Scope isolate_scope(isolate_);
  v8::HandleScope handle_scope(isolate_);

  isolate_->SetCaptureStackTraceForUncaughtExceptions(true);
  isolate_->SetData(0, this);
  v8::Local<v8::ObjectTemplate> global = v8::ObjectTemplate::New(GetIsolate());

  v8::TryCatch try_catch;

  global->Set(v8::String::NewFromUtf8(GetIsolate(), "log"),
              v8::FunctionTemplate::New(GetIsolate(), Print));
  global->Set(v8::String::NewFromUtf8(GetIsolate(), "N1qlQuery"),
              v8::FunctionTemplate::New(GetIsolate(), N1qlQueryConstructor));

  if (try_catch.HasCaught()) {
    last_exception = ExceptionString(GetIsolate(), &try_catch);
    LOG(logError) << "Last expection: " << last_exception << '\n';
  }

  v8::Local<v8::Context> context = v8::Context::New(GetIsolate(), NULL, global);
  context_.Reset(GetIsolate(), context);

  app_name_ = app_name;
  cb_kv_endpoint = kv_host_port;

  deployment_config *config = ParseDeployment(dep_cfg.c_str());

  cb_source_bucket.assign(config->source_bucket);

  std::map<std::string,
           std::map<std::string, std::vector<std::string>>>::iterator it =
      config->component_configs.begin();

  for (; it != config->component_configs.end(); it++) {

    if (it->first == "buckets") {
      std::map<std::string, std::vector<std::string>>::iterator bucket =
          config->component_configs["buckets"].begin();
      for (; bucket != config->component_configs["buckets"].end(); bucket++) {
        std::string bucket_alias = bucket->first;
        std::string bucket_name =
            config->component_configs["buckets"][bucket_alias][0];

        bucket_handle =
            new Bucket(this, bucket_name.c_str(), cb_kv_endpoint.c_str(),
                       bucket_alias.c_str(), rbac_user, rbac_pass);
      }
    }
  }
  delete config;

  LOG(logInfo) << "Initialised V8Worker handle, app_name: " << app_name
               << " kv_host_port: " << kv_host_port
               << " rbac_user: " << rbac_user << " rbac_pass: " << rbac_pass
               << '\n';

  n1ql_handle =
      new N1QL(cb_kv_endpoint, cb_source_bucket, rbac_user, rbac_pass);
}

V8Worker::~V8Worker() {
  context_.Reset();
  on_update_.Reset();
  on_delete_.Reset();
}

std::string GetWorkingPath() {
  char temp[MAXPATHLEN];
  return (getcwd(temp, MAXPATHLEN) ? std::string(temp) : std::string(""));
}

int V8Worker::V8WorkerLoad(std::string script_to_execute) {
  LOG(logInfo) << "getcwd: " << GetWorkingPath() << '\n';
  v8::Locker locker(GetIsolate());
  v8::Isolate::Scope isolate_scope(GetIsolate());
  v8::HandleScope handle_scope(GetIsolate());

  v8::Local<v8::Context> context =
      v8::Local<v8::Context>::New(GetIsolate(), context_);
  v8::Context::Scope context_scope(context);

  v8::TryCatch try_catch;

  std::string plain_js;
  int code = Jsify(script_to_execute.c_str(), &plain_js);
  LOG(logTrace) << "jsified code: " << plain_js << '\n';
  if (code != OK) {
    LOG(logError) << "failed to jsify: " << code << '\n';
    return code;
  }

  std::string transpiler_js_src = ReadFile(TRANSPILER_JS_PATH);
  transpiler_js_src += ReadFile(ESTOOLS_PATH);
  script_to_execute = Transpile(transpiler_js_src, plain_js, EXEC_TRANSPILER);

  v8::Local<v8::String> source =
      v8::String::NewFromUtf8(GetIsolate(), script_to_execute.c_str());

  script_to_execute_ = script_to_execute;
  LOG(logTrace) << "script to execute: " << script_to_execute << '\n';

  if (!ExecuteScript(source))
    return FAILED_TO_COMPILE_JS;

  v8::Local<v8::String> on_update =
      v8::String::NewFromUtf8(GetIsolate(), "OnUpdate",
                              v8::NewStringType::kNormal)
          .ToLocalChecked();

  v8::Local<v8::String> on_delete =
      v8::String::NewFromUtf8(GetIsolate(), "OnDelete",
                              v8::NewStringType::kNormal)
          .ToLocalChecked();

  v8::Local<v8::Value> on_update_val;
  v8::Local<v8::Value> on_delete_val;

  if (!context->Global()->Get(context, on_update).ToLocal(&on_update_val) ||
      !context->Global()->Get(context, on_delete).ToLocal(&on_delete_val)) {
    return NO_HANDLERS_DEFINED;
  }

  v8::Local<v8::Function> on_update_fun =
      v8::Local<v8::Function>::Cast(on_update_val);
  on_update_.Reset(GetIsolate(), on_update_fun);

  v8::Local<v8::Function> on_delete_fun =
      v8::Local<v8::Function>::Cast(on_delete_val);
  on_delete_.Reset(GetIsolate(), on_delete_fun);

  if (bucket_handle) {
    if (!bucket_handle->Initialize(this, &bucket)) {
      LOG(logError) << "Error initializing bucket handle" << '\n';
      return FAILED_INIT_BUCKET_HANDLE;
    }
  }

  if (n1ql_handle) {
    if (!n1ql_handle->GetInitStatus()) {
      LOG(logError) << "Error initializing n1ql handle" << '\n';
    }
  }

  return SUCCESS;
}

bool V8Worker::ExecuteScript(v8::Local<v8::String> script) {
  v8::HandleScope handle_scope(GetIsolate());

  v8::TryCatch try_catch(GetIsolate());

  v8::Local<v8::Context> context(GetIsolate()->GetCurrentContext());

  v8::Local<v8::Script> compiled_script;
  if (!v8::Script::Compile(context, script).ToLocal(&compiled_script)) {
    assert(try_catch.HasCaught());
    last_exception = ExceptionString(GetIsolate(), &try_catch);
    LOG(logError) << "Exception logged:" << last_exception << '\n';
    // The script failed to compile; bail out.
    return false;
  }

  v8::Local<v8::Value> result;
  if (!compiled_script->Run(context).ToLocal(&result)) {
    assert(try_catch.HasCaught());
    last_exception = ExceptionString(GetIsolate(), &try_catch);
    LOG(logError) << "Exception logged:" << last_exception << '\n';
    // Running the script failed; bail out.
    return false;
  }
  return true;
}

int V8Worker::SendUpdate(std::string value, std::string meta,
                         std::string doc_type) {
  v8::Locker locker(GetIsolate());
  v8::Isolate::Scope isolate_scope(GetIsolate());
  v8::HandleScope handle_scope(GetIsolate());

  v8::Local<v8::Context> context =
      v8::Local<v8::Context>::New(GetIsolate(), context_);
  v8::Context::Scope context_scope(context);

  LOG(logTrace) << "value: " << value << " meta: " << meta
                << " doc_type: " << doc_type << '\n';
  v8::TryCatch try_catch(GetIsolate());

  v8::Handle<v8::Value> args[2];
  if (doc_type.compare("json") == 0) {
    args[0] =
        v8::JSON::Parse(v8::String::NewFromUtf8(GetIsolate(), value.c_str()));
  } else {
    args[0] = v8::String::NewFromUtf8(GetIsolate(), value.c_str());
  }

  args[1] =
      v8::JSON::Parse(v8::String::NewFromUtf8(GetIsolate(), meta.c_str()));

  if (try_catch.HasCaught()) {
    last_exception = ExceptionString(GetIsolate(), &try_catch);
    LOG(logError) << "Last exception: " << last_exception << '\n';
  }

  v8::Local<v8::Function> on_doc_update =
      v8::Local<v8::Function>::New(GetIsolate(), on_update_);
  on_doc_update->Call(context->Global(), 2, args);

  if (try_catch.HasCaught()) {
    LOG(logDebug) << "Exception message: "
                  << ExceptionString(GetIsolate(), &try_catch) << '\n';
    return ON_UPDATE_CALL_FAIL;
  }

  return SUCCESS;
}

int V8Worker::SendDelete(std::string meta) {
  v8::Locker locker(GetIsolate());
  v8::Isolate::Scope isolate_scope(GetIsolate());
  v8::HandleScope handle_scope(GetIsolate());

  v8::Local<v8::Context> context =
      v8::Local<v8::Context>::New(GetIsolate(), context_);
  v8::Context::Scope context_scope(context);

  LOG(logTrace) << " meta: " << meta << '\n';
  v8::TryCatch try_catch(GetIsolate());

  v8::Local<v8::Value> args[1];
  args[0] =
      v8::JSON::Parse(v8::String::NewFromUtf8(GetIsolate(), meta.c_str()));

  assert(!try_catch.HasCaught());

  v8::Local<v8::Function> on_doc_delete =
      v8::Local<v8::Function>::New(GetIsolate(), on_delete_);
  on_doc_delete->Call(context->Global(), 1, args);

  if (try_catch.HasCaught()) {
    LOG(logError) << "Exception message"
                  << ExceptionString(GetIsolate(), &try_catch) << '\n';
    return ON_DELETE_CALL_FAIL;
  }

  return SUCCESS;
}

const char *V8Worker::V8WorkerLastException() { return last_exception.c_str(); }

const char *V8Worker::V8WorkerVersion() { return v8::V8::GetVersion(); }

void V8Worker::V8WorkerTerminateExecution() {
  v8::V8::TerminateExecution(GetIsolate());
}