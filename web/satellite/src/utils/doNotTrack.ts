// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

export function isDoNotTrackEnabled () {
    const doNotTrackOption = (
      window.doNotTrack ||
      window.navigator.doNotTrack
    );
  
    if (!doNotTrackOption) {
      return false;
    }
  
    if (
      doNotTrackOption.charAt(0)  === '1' ||
      doNotTrackOption === 'yes'
    ) {
      return true;
    }
  
    return false;
}
