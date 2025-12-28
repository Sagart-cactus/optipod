/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package application

// This file previously contained tests for memory safety checks that have been removed
// as part of the memory safety enhancements feature. The safety check functions
// isUnsafeMemoryDecrease and isUnsafeRequestDecrease have been removed from the engine
// to allow direct application of recommendations without conservative blocking.
//
// The new approach trusts usage-based recommendations and applies sensible default
// limit configurations instead of blocking potentially safe optimizations.
