// Copyright (c) 2019 Couchbase, Inc.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//     http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an "AS IS"
// BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express
// or implied. See the License for the specific language governing
// permissions and limitations under the License.

#ifndef COUCHBASE_TIMER_STORE_H
#define COUCHBASE_TIMER_STORE_H

#include <libcouchbase/api3.h>
#include <mutex>
#include <string>
#include <unordered_map>
#include <unordered_set>
#include <v8.h>

#include "lcb_utils.h"
#include "timer_defs.h"
#include "timer_iterator.h"

namespace timer {

class TimerStore {
public:
  explicit TimerStore(v8::Isolate *isolate, const std::string &prefix,
                      const std::vector<int64_t> &partitions,
                      const std::string &conn_str);
  ~TimerStore();

  lcb_error_t SetTimer(TimerInfo &timer);

  void DeleteTimer(TimerEvent &event);

  Iterator GetIterator();

  void UpdatePartition(const std::unordered_set<int64_t> &partitions);

  void SyncSpan();

private:
  void Connect();
  std::pair<bool, lcb_error_t> SyncSpan(int partition);

  bool ExpandSpan(int64_t partition, int64_t point);

  void ShrinkSpan(int64_t partition, int64_t start);

  std::pair<lcb_error_t, Result> GetCounter(const std::string &key);

  std::pair<lcb_error_t, Result> Insert(const std::string &key,
                                        const nlohmann::json &value);

  std::pair<lcb_error_t, Result> Upsert(const std::string &key,
                                        const nlohmann::json &value);
  std::pair<lcb_error_t, Result>
  Replace(const std::string &key, const nlohmann::json &value, lcb_CAS cas);

  lcb_error_t Delete(const std::string &key, uint64_t cas);

  std::pair<lcb_error_t, Result> Get(const std::string &key);

  v8::Isolate *isolate_;
  std::unordered_set<int64_t> new_partions_;
  std::unordered_set<int64_t> partitons_;
  std::atomic_bool is_dirty_;
  std::unordered_map<int64_t, TimerSpan> span_map_;
  std::string prefix_;
  std::string conn_str_;
  lcb_t crud_handle_{nullptr};
  std::mutex sync_lock;
  friend class Iterator;
};
} // namespace timer
#endif // COUCHBASE_TIMER_STORE_H
