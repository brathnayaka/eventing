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

#ifndef FUNCTION_TEMPLATES_H
#define FUNCTION_TEMPLATES_H

#include <libcouchbase/api3.h>
#include <libcouchbase/couchbase.h>
#include <libplatform/libplatform.h>
#include <thread>
#include <v8.h>

#include "log.h"
#include "utils.h"
#include "v8worker.h"

#define CONSOLE_LOG_MAX_ARITY 20

extern long curl_timeout;

struct Result {
  lcb_CAS cas;
  lcb_error_t rc;
  std::string value;
  uint32_t exptime;

  Result() : cas(0), rc(LCB_SUCCESS) {}
};

struct CurlResult {
  char *memory;
  size_t size;
};

void Curl(const v8::FunctionCallbackInfo<v8::Value> &args);

void Log(const v8::FunctionCallbackInfo<v8::Value> &args);
void ConsoleLog(const v8::FunctionCallbackInfo<v8::Value> &args);

#endif
